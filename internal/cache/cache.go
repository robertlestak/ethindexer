package cache

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis"

	log "github.com/sirupsen/logrus"
)

var (
	Client        *redis.Client
	cacheTTL      = time.Minute * 10
	cacheFlushTTL time.Duration
	NeedsFlush    = make(map[string]bool)
	mutex         = &sync.Mutex{}
)

const (
	cachePrefix = "cache:"
)

func Init() error {
	l := log.WithFields(log.Fields{
		"package": "cache",
	})
	l.Info("Initializing redis client")
	Client = redis.NewClient(&redis.Options{
		Addr:        fmt.Sprintf("%s:%s", os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PORT")),
		Password:    "", // no password set
		DB:          0,  // use default DB
		DialTimeout: 30 * time.Second,
		ReadTimeout: 30 * time.Second,
	})
	cmd := Client.Ping()
	if cmd.Err() != nil {
		l.Error("Failed to connect to redis")
		return cmd.Err()
	}
	l.Info("Connected to redis")
	if os.Getenv("CACHE_TTL") != "" {
		var err error
		cacheTTL, err = time.ParseDuration(os.Getenv("CACHE_TTL"))
		if err != nil {
			l.WithError(err).Error("failed to parse CACHE_TTL")
			return err
		}
	}
	if os.Getenv("CACHE_FLUSH_TTL") != "" {
		var err error
		cacheFlushTTL, err = time.ParseDuration(os.Getenv("CACHE_FLUSH_TTL"))
		if err != nil {
			l.WithError(err).Error("failed to parse CACHE_FLUSH_TTL")
			return err
		}
	}
	if cacheFlushTTL != 0 {
		go Flusher()
	}
	return nil
}

func Get(key string) (string, error) {
	l := log.WithFields(log.Fields{
		"package": "cache",
	})
	l.Debug("Getting key from redis")
	cmd := Client.Get(cachePrefix + key)
	if cmd.Err() != nil {
		l.Error("Failed to get key from redis")
		return "", cmd.Err()
	}
	l.Debug("Got key from redis")
	return cmd.Result()
}

func Set(key string, value string, exp time.Duration) error {
	l := log.WithFields(log.Fields{
		"package": "cache",
	})
	l.Debug("Setting key in redis")
	cmd := Client.Set(cachePrefix+key, value, exp)
	if cmd.Err() != nil {
		l.Error("Failed to set key in redis")
		return cmd.Err()
	}
	l.Debug("Set key in redis")
	return nil
}

func cleanReq(req *http.Request) {
	l := log.WithFields(log.Fields{
		"package": "proxy",
		"method":  "cleanReq",
	})
	l.Debug("start")
	var protected = []string{
		"Authorization",
		"Content-Type",
		"Accept",
		"Content-Length",
		"Content-Encoding",
	}
	l.WithField("protected", protected).Debug("remove headers")
	for n := range req.Header {
		l.WithField("header", n).Debug("check header")
		var found bool
	RangeProtected:
		for _, p := range protected {
			if n == p {
				found = true
				break RangeProtected
			}
		}
		if !found {
			l.WithField("header", n).Debug("remove header")
			req.Header.Del(n)
		}
	}
}

func ResponseFromCache(req *http.Request) ([]byte, error) {
	chain := req.URL.Query().Get("chain")
	l := log.WithFields(log.Fields{
		"package": "proxy",
		"method":  "ResponseFromCache",
		"chain":   chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	var bd []byte
	var err error
	if req.Body != nil {
		cleanReq(req)
		bd, err = httputil.DumpRequest(req, true)
		if err != nil {
			l.WithError(err).Error("failed to dump request")
			return nil, err
		}
		l.WithField("body", string(bd)).Debug("dumped request")
	}
	rh := fmt.Sprintf("%x", md5.Sum(bd))
	cacheKey := chain + ":" + rh
	cd, err := Get(cacheKey)
	if err != nil {
		l.WithError(err).Error("failed to get cache")
		return nil, err
	}
	decoded, derr := base64.StdEncoding.DecodeString(cd)
	if derr != nil {
		l.WithError(derr).Error("decode cache")
		return nil, derr
	}
	return decoded, nil
}

func CacheReqResponse(req *http.Request, rd []byte) error {
	chain := req.URL.Query().Get("chain")
	l := log.WithFields(log.Fields{
		"package": "proxy",
		"method":  "CacheReqResponse",
		"chain":   chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	var bd []byte
	var err error
	if req.Body != nil {
		cleanReq(req)
		bd, err = httputil.DumpRequest(req, true)
		if err != nil {
			l.WithError(err).Error("failed to dump request")
			return err
		}
		l.WithField("body", string(bd)).Debug("dumped request")
	}
	rh := fmt.Sprintf("%x", md5.Sum(bd))
	cacheKey := chain + ":" + rh
	rds := string(rd)
	if strings.TrimSpace(rds) == "" || strings.TrimSpace(rds) == "null" {
		l.Debug("empty response")
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString(rd)
	err = Set(cacheKey, encoded, cacheTTL)
	if err != nil {
		l.WithError(err).Error("failed to set cache")
		return err
	}
	return nil
}

func FlushChainCache(chain string) error {
	l := log.WithFields(log.Fields{
		"package": "proxy",
		"method":  "FlushChainCache",
		"chain":   chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	var keys []string
	var cursor uint64
	cacheKey := cachePrefix + chain
	l.Debugf("cache key: %s", cacheKey)
	var initCursor bool = true
	for cursor > 0 || initCursor {
		scmd := Client.Scan(cursor, cacheKey+":*", 1000)
		if scmd.Err() != nil && scmd.Err() != redis.Nil {
			l.Error("Failed to get chain cache keys keys")
			return scmd.Err()
		}
		var lblocks []string
		lblocks, cursor = scmd.Val()
		keys = append(keys, lblocks...)
		initCursor = false
		l.Debugf("cursor: %d", cursor)
		l.Debugf("keys: %v", len(keys))
	}
	for _, key := range keys {
		cmd := Client.Del(key)
		if cmd.Err() != nil && cmd.Err() != redis.Nil {
			l.Error("Failed to delete key")
			return cmd.Err()
		}
	}
	return nil
}

func Flusher() {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"method":  "Flusher",
	})
	l.Debug("start")
	defer l.Debug("end")
	for {
		l.Debug("Flushing cache")
		for k, v := range NeedsFlush {
			l.Debugf("Flushing chain %s", k)
			if v {
				err := FlushChainCache(k)
				if err != nil {
					l.WithError(err).Error("failed to flush chain cache")
				}
				NeedsFlush[k] = false
			}
		}
		time.Sleep(cacheFlushTTL)
	}
}

func Flushable(chain string) error {
	l := log.WithFields(log.Fields{
		"package": "cache",
		"method":  "Flushable",
		"chain":   chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	mutex.Lock()
	NeedsFlush[chain] = true
	mutex.Unlock()
	return nil
}

func Middleware(h http.Handler) http.Handler {
	l := log.WithFields(log.Fields{
		"package": "proxy",
		"method":  "Middleware",
	})
	l.Debug("start")
	defer l.Debug("end")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("nocache") == "true" {
			l.Debug("nocache is true")
			h.ServeHTTP(w, r)
			return
		}
		if resp, err := ResponseFromCache(r); err == nil {
			l.Debug("cache hit")
			l.Debugf("%+v", resp)
			w.Header().Set("x-ethindexer-cache", "hit")
			w.Write(resp)
			return
		} else {
			l.WithError(err).Error("cache miss")
			w.Header().Set("x-ethindexer-cache", "miss")
		}
		h.ServeHTTP(w, r)
	})
}
