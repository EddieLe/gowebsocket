package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	r "github.com/garyburd/redigo/redis"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
)

var defaultYaml = []byte(`
	REDIS_HOST: 127.0.0.1
	REDIS_PORT: 6379
	`)

//本機測試
var addr = flag.String("addr", "localhost:8080", "http service address")
var upgrader = websocket.Upgrader{}
var homeTemplate = template.Must(template.New("").Parse(`
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<script>  
window.addEventListener("load", function(evt) {
    var output = document.getElementById("output");
    var input = document.getElementById("input");
    var ws;
    var print = function(message) {
        var d = document.createElement("div");
        d.innerHTML = message;
        output.appendChild(d);
    };
    document.getElementById("open").onclick = function(evt) {
        if (ws) {
            return false;
        }
        ws = new WebSocket("{{.}}");
        ws.onopen = function(evt) {
            print("OPEN");
        }
        ws.onclose = function(evt) {
            print("CLOSE");
            ws = null;
        }
        ws.onmessage = function(evt) {
            print("RESPONSE: " + evt.data);
        }
        ws.onerror = function(evt) {
            print("ERROR: " + evt.data);
        }
        return false;
    };
    document.getElementById("send").onclick = function(evt) {
        if (!ws) {
            return false;
        }
        print("SEND: " + input.value);
        ws.send(input.value);
        return false;
    };
    document.getElementById("close").onclick = function(evt) {
        if (!ws) {
            return false;
        }
        ws.close();
        return false;
    };
});
</script>
</head>
<body>
<table>
<form>
<button id="open">Open</button>
<button id="close">Close</button>
<p><input id="input" type="text" value="" size="80">
<button id="send">Send</button>
</form>
</td><td valign="top" width="50%">
<div id="output"></div>
</td></tr></table>
</body>
</html>
`))

func setRedis(domain int, amount int) {
	c, err := r.Dial("tcp", viper.GetString("REDIS_HOST")+":"+viper.GetString("REDIS_PORT"))
	if err != nil {
		fmt.Println("connect to redis err", err.Error())
		return
	}
	defer c.Close()
	//寫redis hash incrby
	_, setRedisErr := c.Do("hincrby", "order", domain, amount)
	if setRedisErr != nil {
		fmt.Println("hincrby error", setRedisErr.Error())
	}
}

func getRedis(domain int) {
	c, err := r.Dial("tcp", viper.GetString("REDIS_HOST")+":"+viper.GetString("REDIS_PORT"))
	if err != nil {
		fmt.Println("connect to redis err", err.Error())
		return
	}
	defer c.Close()
	value, readRedisErr := r.Values(c.Do("hmget", "order", domain))
	if readRedisErr != nil {
		fmt.Println("hmget failed", readRedisErr.Error())
	} else {
		fmt.Printf("domain ID: %d amount: ", domain)
		for _, v := range value {
			fmt.Printf("%s ", v.([]byte))
		}
		fmt.Print("\n")
	}
}

func write(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		jsonToMapWrite(w, message)
		log.Printf("recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Println("write:", err)
			break
		}
	}
}

func read(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		jsonToMapRead(w, message)
		log.Printf("recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Println("write:", err)
			break
		}
	}
}

func writeRedis(w http.ResponseWriter, r *http.Request) {
	homeTemplate.Execute(w, "ws://"+r.Host+"/write")
}
func readRedis(w http.ResponseWriter, r *http.Request) {
	homeTemplate.Execute(w, "ws://"+r.Host+"/read")
}

func jsonToMapWrite(w http.ResponseWriter, data []byte) {
	mData := make(map[string]int)
	err := json.Unmarshal(data, &mData)
	for k, _ := range mData {
		if string(mData[k]) == "" {
			ErrorResponse(w, http.StatusForbidden, 10002, "參數不為空")
			return
		}
	}

	if err != nil {
		ErrorResponse(w, http.StatusForbidden, 10001, "JSON Umarshal failed")
		fmt.Println("Umarshal failed:", err)
		return
	}

	//寫redis
	setRedis(mData["domain"], mData["amount"])
	SuccessResponse(w, "success")
}

func jsonToMapRead(w http.ResponseWriter, data []byte) {
	mData := make(map[string]int)
	err := json.Unmarshal(data, &mData)
	for k, _ := range mData {
		if string(mData[k]) == "" {
			ErrorResponse(w, http.StatusForbidden, 10002, "參數不為空")
			return
		}
	}

	if err != nil {
		ErrorResponse(w, http.StatusForbidden, 10001, "JSON Umarshal failed")
		fmt.Println("Umarshal failed:", err)
		return
	}

	//讀redis
	getRedis(mData["domain"])
	SuccessResponse(w, "success")
}

func SuccessResponse(w http.ResponseWriter, data interface{}) {
	res := make(map[string]interface{})
	res["success"] = data
	resStr, _ := json.Marshal(res)
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, string(resStr[:]))
	log.Printf("HTTP response:\n%s\n", string(resStr[:]))
}

func ErrorResponse(w http.ResponseWriter, httpStatus int, errorCode int, msg string) {
	errorRes := make(map[string]interface{})
	errorRes["error_code"] = errorCode
	errorRes["error_message"] = msg
	errorRes["data"] = nil

	res := make(map[string]interface{})
	res["error"] = errorRes

	resStr, _ := json.Marshal(res)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	io.WriteString(w, string(resStr[:]))
	log.Printf("HTTP response:\n%s\n", string(resStr[:]))
}

func main() {
	//先使用ENV
	viper.AutomaticEnv()

	//yaml設定
	viper.SetConfigType("yaml")
	var configFile string
	flag.StringVar(&configFile, "c", "config.yaml", "a string var")

	if configFile != "" {
		content, err := ioutil.ReadFile(configFile)
		if err != nil {
			content = defaultYaml
		}

		viper.ReadConfig(bytes.NewBuffer(content))
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigName("config")
		if err := viper.ReadInConfig(); err == nil {
			fmt.Println("Using config file:", viper.ConfigFileUsed())
		} else {
			// load default config
			viper.ReadConfig(bytes.NewBuffer(defaultYaml))
		}
	}
	//WS
	flag.Parse()
	log.SetFlags(0)
	http.HandleFunc("/write", write)
	http.HandleFunc("/read", read)
	http.HandleFunc("/writeRedis", writeRedis)
	http.HandleFunc("/readRedis", readRedis)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
