package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/zhangjie2012/logrusredis"
	"gopkg.in/yaml.v2"
)

var (
	config      = "/etc/loki-redis-reporter/config.yaml"
	lokiPushUrl = ""
)

type Config struct {
	R struct {
		Host     string   `yaml:"host"`
		Password string   `yaml:"password"`
		DB       int      `yaml:"db"`
		Keys     []string `yaml:"keys"`
	} `yaml:"redis"`
	L struct {
		Server string `yaml:"server"`
	} `yaml:"loki"`
}

func getConfig(config string) (*Config, error) {
	bs, err := ioutil.ReadFile(config)
	if err != nil {
		return nil, err
	}
	c := Config{}
	if err := yaml.Unmarshal(bs, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func consumeAndReport(ctx context.Context, wg *sync.WaitGroup, c *redis.Client, key string) {
	defer wg.Done()

	blockD := 100 * time.Millisecond

newLoop:
	for {
		select {
		case <-ctx.Done():
			log.Printf("receive done => %s", key)
			return
		default:
			bs, err := c.RPop(key).Bytes()
			if err != nil && err != redis.Nil {
				log.Printf("redis rpop failure, close this routine, key=%s, err=%s", key, err)
				return // !!!!
			}

			if len(bs) == 0 {
				time.Sleep(blockD)
				promIdleCountInc()
				goto newLoop
			}

			logS := logrusredis.LogS{}
			if err := json.Unmarshal(bs, &logS); err != nil {
				// must be dirty data, just throw it
				log.Printf("data unmarshal failure, key=%s, err=%s", key, err)
				promDirtyCountInc()
				goto newLoop
			}

			if err := reportLoki(&logS); err != nil {
				// no matter what error, we need push back `logS` to redis, and stop consume
				//   or log data will lost
				_, e := c.RPush(key, bs).Result()
				log.Printf("write loki failure, err=%s, will exit service, write log back result = %s", err, e)
				return // !!!!
			}

			promSuccCountInc(key)
		}
	}
}

// https://github.com/grafana/loki/blob/v1.5.0/docs/api.md#post-lokiapiv1push
// {
//   "streams": [
//     {
//       "stream": {
//         "label": "value"
//       },
//       "values": [
//           [ "<unix epoch in nanoseconds>", "<log line>" ],
//           [ "<unix epoch in nanoseconds>", "<log line>" ]
//       ]
//     }
//   ]
// }

type ValueT []string

type StreamT struct {
	Stream logrus.Fields `json:"stream"`
	Values []ValueT      `json:"values"`
}

type ReqBody struct {
	Streams []StreamT `json:"streams"`
}

func reportLoki(logS *logrusredis.LogS) error {
	r := ReqBody{}
	// stream fill
	r.Streams = make([]StreamT, 1)
	r.Streams[0].Stream = logrus.Fields{}
	r.Streams[0].Stream["_appname"] = logS.AppName
	r.Streams[0].Stream["_ip"] = logS.Ip
	r.Streams[0].Stream["_level"] = logS.Level
	r.Streams[0].Stream["_caller"] = logS.Caller
	// '_' prefix will make sure unique with `metadata`
	for key, value := range logS.MetaData {
		if key == "" {
			continue
		}
		// not string, will Marshal to json string, if err, no nothing
		switch v := value.(type) {
		case string:
			r.Streams[0].Stream[key] = v
		default:
			bs, err := json.Marshal(value)
			if err == nil {
				r.Streams[0].Stream[key] = string(bs)
			} else {
				promWrongMetaDataInc()
			}
		}
	}

	// values fill
	r.Streams[0].Values = make([]ValueT, 1)
	r.Streams[0].Values[0] = ValueT{
		strconv.FormatInt(logS.Timestamp, 10),
		logS.Msg,
	}

	// push message
	bs, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := httpPost(lokiPushUrl, bs); err != nil {
		return err
	}
	return nil
}

func main() {
	flag.StringVar(&config, "config", config, "the config file")
	flag.Parse()
	config, err := getConfig(config)
	if err != nil {
		log.Fatalf("config parse error, %s", err)
	}

	if len(config.R.Keys) == 0 {
		log.Println("at least one key")
		return
	}

	// redis client is safe for multiple goroutines
	rClient := redis.NewClient(&redis.Options{
		Addr:     config.R.Host,
		Password: config.R.Password,
		DB:       config.R.DB,
	})
	if _, err := rClient.Ping().Result(); err != nil {
		log.Fatalf("redis connect error %s", err)
	}
	defer rClient.Close()

	// try to connect loki by ready method
	// https://github.com/grafana/loki/blob/v1.5.0/docs/api.md#get-ready
	if code, err := httpGetStatusCode(fmt.Sprintf("%s/ready", config.L.Server)); err != nil && code != 200 {
		log.Fatalf("loki server not ready %s", err)
	}
	lokiPushUrl = fmt.Sprintf("%s/loki/api/v1/push", config.L.Server)

	ctx, cancel := context.WithCancel(context.Background())
	wg := sync.WaitGroup{}
	for _, key := range config.R.Keys {
		wg.Add(1)
		go consumeAndReport(ctx, &wg, rClient, key)
	}

	// Expose the registered metrics via HTTP.
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Fatal(http.ListenAndServe(":6666", nil))
	}()

	go printConsumeStat()

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Ignore(syscall.SIGPIPE)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		fmt.Println("receive shutdown")
		cancel()
	}()

	wg.Wait()
}
