// Copyright 2018 aletheia7. All rights reserved. Use of this source code is
// governed by a BSD-2-Clause license that can be found in the LICENSE file.

// Provides a message bus for publish/subscribe messaging.
package mbus

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/k-sone/critbitgo"
)

const (
	Sub_channel_prefix   = "sub_"
	Unsub_channel_prefix = "unsub_"
	pub_timer_fail       = time.Second
)

type Bus struct {
	Drop_slow_consumers bool
	ctx                 context.Context
	ch                  chan interface{}
	topic_ct            int64
	trie                *critbitgo.Trie
	w                   interface{}
}

type Warner interface {
	Warning(a ...interface{})
}

func New_bus(ctx context.Context, warner interface{}) *Bus {
	r := &Bus{
		ctx:  ctx,
		ch:   make(chan interface{}, 256),
		trie: critbitgo.NewTrie(),
		w:    warner,
	}
	go r.loop()
	return r
}

func (o *Bus) Next() string {
	return strconv.FormatInt(atomic.AddInt64(&o.topic_ct, 1), 10)
}

func (o *Bus) pub(m *Msg) {
	if v, ok := o.trie.Get([]byte(m.Topic)); ok {
		for ch := range v.(map[chan *Msg]bool) {
			select {
			case <-o.ctx.Done():
				return
			default:
				if o.Drop_slow_consumers {
					select {
					case <-o.ctx.Done():
						return
					case ch <- m:
					default:
						// ch: slow consumer or no chan receiver
						select {
						case <-o.ctx.Done():
							return
						case ch <- m:
						case <-time.After(pub_timer_fail):
							if v, ok := o.trie.Get([]byte(m.Topic)); ok && v.(map[chan *Msg]bool)[ch] {
								if w, ok := o.w.(Warner); ok {
									w.Warning("cannot pub, increase chan size:", len(ch), ch, m.Topic)
								}
								o.do_sub(&subscription{topics: []string{m.Topic}, c: ch})
							}
						}
					}
				} else {

					select {
					case <-o.ctx.Done():
					case ch <- m:
					}
				}
			}
		}
	}
}

func (o *Bus) loop() {
	defer o.trie.Clear()
	for {
		select {
		case <-o.ctx.Done():
			return
		case i := <-o.ch:
			switch t := i.(type) {
			case *Msg:
				o.pub(t)
			case *subscription:
				o.do_sub(t)
			}
		}
	}
}

func (o *Bus) do_sub(m *subscription) {
	if m.on {
		for _, topic := range m.topics {
			select {
			case <-o.ctx.Done():
				return
			default:
				key := []byte(topic)
				if v, ok := o.trie.Get(key); ok {
					v.(map[chan *Msg]bool)[m.c] = true
					o.trie.Set(key, v)
				} else {
					o.trie.Set(key, map[chan *Msg]bool{m.c: true})
				}
				o.pub(New_msg(Sub_channel_prefix+topic, m.c))
			}
		}
	} else {
		for _, topic := range m.topics {
			select {
			case <-o.ctx.Done():
				return
			default:
				key := []byte(topic)
				if v, ok := o.trie.Get(key); ok {
					delete(v.(map[chan *Msg]bool), m.c)
					if len(v.(map[chan *Msg]bool)) == 0 {
						o.trie.Delete(key)
					} else {
						o.trie.Set(key, v)
					}
				}
				o.pub(New_msg(Unsub_channel_prefix+topic, m.c))
			}
		}
	}
}

// Pub makes and publishes a new Msg.
// topic must not be nil.
// data should be a *data.
//
func (o *Bus) Pub(topic string, data interface{}) {
	if len(topic) == 0 {
		return
	}
	o.ch <- New_msg(topic, data)
}

func (o *Bus) Pubm(m *Msg) {
	if len(m.Topic) == 0 {
		return
	}
	o.ch <- m
}

type subscription struct {
	topics []string
	c      chan *Msg
	on     bool
}

func (o *Bus) Subscribe(c chan *Msg, topics ...string) {
	o.ch <- &subscription{topics: topics, c: c, on: true}
}

func (o *Bus) Unsubscribe(c chan *Msg, topics ...string) {
	o.ch <- &subscription{topics: topics, c: c}
}

type Msg struct {
	Topic string
	Data  interface{}
}

// New_msg makes a new Msg
// Msg is not published
// Data should be a *data
//
func New_msg(topic string, data interface{}) *Msg {
	return &Msg{Topic: topic, Data: data}
}
