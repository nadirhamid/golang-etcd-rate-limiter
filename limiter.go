package limiter

import (
	"fmt"
	"context"
	"time"
	"strconv"
	"go.etcd.io/etcd/clientv3"
)

type RateLimiter struct {
	Etcd *clientv3.Client

	Limit         uint64
	BaseKey       string
	Interval      time.Duration
	FlushInterval time.Duration
	ticker     *time.Ticker
	stopTicker chan bool
	keys map[string]string
}

// New returns an instance or RateLimiter, which isn't yet initialized
func NewRateLimiter(etcd * clientv3.Client, baseKey string, limit uint64, interval time.Duration, flushInterval time.Duration) *RateLimiter {
	keys:=make(map[string]string)
	keys["requests"]=baseKey+"_requests"
	keys["time_initialized"]=baseKey+"_time_initialized"
	keys["deadline"]=baseKey+"_deadline"
	rl := &RateLimiter{
		Etcd: etcd,
		Limit:         limit,
		BaseKey:       baseKey,
		Interval:      interval,
		FlushInterval: flushInterval,
		keys: keys,
	}

	return rl
}

// Initialize the key if its not set yet
// This is used to initialize the deadline, and request counts
func (rl *RateLimiter) initializeKeyIfNeeded(key string, initValue string) (error) {
	ctx := context.Background()
    kv := clientv3.NewKV(rl.Etcd)
    _, err := kv.Get(ctx, key)
	if err == nil {
		return nil
	}
   	_, err = kv.Put(ctx, key, initValue)
	if err != nil {
		return err
	}
	return nil
}

// this will check if the rate limit's deadline has past. if it has
// it will remove the limits, which will allow new requests to come in
func (rl *RateLimiter) evaluateDeadline() (error) {
    kv := clientv3.NewKV(rl.Etcd)
	ctx:=context.Background()
    gr, err := kv.Get(ctx, rl.keys["deadline"])
	if err != nil {
		return err
	}
	value:=string(gr.Kvs[0].Value)
	deadline,err:=strconv.Atoi(value)
	if err != nil {
		return err
	}
	now := int(time.Now().Unix())
	if deadline >= now {
		kv.Delete(ctx, rl.BaseKey, clientv3.WithPrefix())
	}
	return nil
}
// Updates the current key, based on the base key
func (rl *RateLimiter) incrementCurrentKey() (error) {
	now := int(time.Now().Unix())
	deadline:=int(time.Now().Local().Add(rl.Interval).Unix())
	key:=rl.BaseKey
	ctx:=context.Background()
	ctx, _=context.WithCancel(ctx)
    kv := clientv3.NewKV(rl.Etcd)

	rl.initializeKeyIfNeeded(rl.keys["requests"], "0")
	rl.initializeKeyIfNeeded(rl.keys["time_initialized"], strconv.Itoa(now))
	rl.initializeKeyIfNeeded(rl.keys["deadline"], strconv.Itoa(deadline))

    gr, err := kv.Get(ctx, rl.keys["requests"])
	if err != nil {
		return err
	}
	value:=string(gr.Kvs[0].Value)
	requests,err:=strconv.Atoi(value)
	if err != nil {
		return err
	}
	requests=requests+1
   	_, err = kv.Put(ctx, key, strconv.Itoa( requests ))
	if err != nil {
		return err
	}
	return nil
}

// IsOverLimit checks if we are over the limit we have set
func (rl *RateLimiter) IsOverLimit() (bool,error) {
	ctx:=context.Background()
    kv := clientv3.NewKV(rl.Etcd)
	gr,err:=kv.Get(ctx,rl.keys["requests"])
	if err != nil {
		return false,err
	}
	value:=string(gr.Kvs[0].Value)
	requests,err:=strconv.Atoi(value)
	if err != nil {
		return false,err
	}
	if requests > int(rl.Limit) {
		return true,nil
	}
	return false,nil
}

// Init starts the ticker, which takes care of periodically flushing/syncing the counter
func (rl *RateLimiter) ProcessLimits() error {
	if rl.Interval < time.Minute {
		return fmt.Errorf("Minimum interval is 1 minute")
	}
	err:=rl.evaluateDeadline()
	if err != nil {
		return err
	}

	rl.incrementCurrentKey()
	return nil
}