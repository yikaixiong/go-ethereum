//版权所有2016年作者
//此文件是Go-Ethereum库的一部分。
//
// Go-Ethereum库是免费软件：您可以重新分发它和/或修改
//根据GNU较少的通用公共许可条款的条款，
//免费软件基金会（许可证的3版本）或
//（根据您的选择）任何以后的版本。
//
// go-ethereum库是为了希望它有用，
//但没有任何保修；甚至没有暗示的保证
//适合或适合特定目的的健身。看到
// GNU较少的通用公共许可证以获取更多详细信息。
//
//您应该收到GNU较少的通用公共许可证的副本
//与Go-Ethereum库一起。如果不是，请参见<http://www.gnu.org/licenses/>。

package event

import (
	"context"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/mclock"
)

//订阅代表事件流。事件的载体通常是
//频道，但不是接口的一部分。
//
//建立时订阅可能会失败。通过错误报告失败
// 渠道。如果订阅存在问题，它将获得值（例如
//交付事件的网络连接已关闭）。只有一个值
// 发送。
//
//当订阅成功结束时，错误通道已关闭（即
//事件来源已关闭）。当调用取消订阅时，它也会关闭。
//
//取消订阅方法取消事件的发送。您必须致电全部订阅
//确保发布与订阅有关的资源的情况。有可能
//调用任何次数。
type Subscription interface {
	Err() <-chan error // returns the error channel
	Unsubscribe()      // cancels sending of events, closing the error channel
}

// Newsubscription在新Goroutine中作为订阅运行生产者功能。这
//给出生产者的频道在调用取消订阅时关闭。如果FN返回
//错误，它是在订阅的错误频道上发送的。
func NewSubscription(producer func(<-chan struct{}) error) Subscription {
	s := &funcSub{unsub: make(chan struct{}), err: make(chan error, 1)}
	go func() {
		defer close(s.err)
		err := producer(s.unsub)
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.unsubscribed {
			if err != nil {
				s.err <- err
			}
			s.unsubscribed = true
		}
	}()
	return s
}

type funcSub struct {
	unsub        chan struct{}
	err          chan error
	mu           sync.Mutex
	unsubscribed bool
}

func (s *funcSub) Unsubscribe() {
	s.mu.Lock()
	if s.unsubscribed {
		s.mu.Unlock()
		return
	}
	s.unsubscribed = true
	close(s.unsub)
	s.mu.Unlock()
	// Wait for producer shutdown.
	<-s.err
}

func (s *funcSub) Err() <-chan error {
	return s.err
}

// Resubscribe calls fn repeatedly to keep a subscription established. When the
// subscription is established, Resubscribe waits for it to fail and calls fn again. This
// process repeats until Unsubscribe is called or the active subscription ends
// successfully.
//
// Resubscribe applies backoff between calls to fn. The time between calls is adapted
// based on the error rate, but will never exceed backoffMax.
func Resubscribe(backoffMax time.Duration, fn ResubscribeFunc) Subscription {
	return ResubscribeErr(backoffMax, func(ctx context.Context, _ error) (Subscription, error) {
		return fn(ctx)
	})
}

// A ResubscribeFunc attempts to establish a subscription.
type ResubscribeFunc func(context.Context) (Subscription, error)

// ResubscribeErr calls fn repeatedly to keep a subscription established. When the
// subscription is established, ResubscribeErr waits for it to fail and calls fn again. This
// process repeats until Unsubscribe is called or the active subscription ends
// successfully.
//
// The difference between Resubscribe and ResubscribeErr is that with ResubscribeErr,
// the error of the failing subscription is available to the callback for logging
// purposes.
//
// ResubscribeErr applies backoff between calls to fn. The time between calls is adapted
// based on the error rate, but will never exceed backoffMax.
func ResubscribeErr(backoffMax time.Duration, fn ResubscribeErrFunc) Subscription {
	s := &resubscribeSub{
		waitTime:   backoffMax / 10,
		backoffMax: backoffMax,
		fn:         fn,
		err:        make(chan error),
		unsub:      make(chan struct{}),
	}
	go s.loop()
	return s
}

// A ResubscribeErrFunc attempts to establish a subscription.
// For every call but the first, the second argument to this function is
// the error that occurred with the previous subscription.
type ResubscribeErrFunc func(context.Context, error) (Subscription, error)

type resubscribeSub struct {
	fn                   ResubscribeErrFunc
	err                  chan error
	unsub                chan struct{}
	unsubOnce            sync.Once
	lastTry              mclock.AbsTime
	lastSubErr           error
	waitTime, backoffMax time.Duration
}

func (s *resubscribeSub) Unsubscribe() {
	s.unsubOnce.Do(func() {
		s.unsub <- struct{}{}
		<-s.err
	})
}

func (s *resubscribeSub) Err() <-chan error {
	return s.err
}

func (s *resubscribeSub) loop() {
	defer close(s.err)
	var done bool
	for !done {
		sub := s.subscribe()
		if sub == nil {
			break
		}
		done = s.waitForError(sub)
		sub.Unsubscribe()
	}
}

func (s *resubscribeSub) subscribe() Subscription {
	subscribed := make(chan error)
	var sub Subscription
	for {
		s.lastTry = mclock.Now()
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			rsub, err := s.fn(ctx, s.lastSubErr)
			sub = rsub
			subscribed <- err
		}()
		select {
		case err := <-subscribed:
			cancel()
			if err == nil {
				if sub == nil {
					panic("event: ResubscribeFunc returned nil subscription and no error")
				}
				return sub
			}
			// Subscribing failed, wait before launching the next try.
			if s.backoffWait() {
				return nil // unsubscribed during wait
			}
		case <-s.unsub:
			cancel()
			<-subscribed // avoid leaking the s.fn goroutine.
			return nil
		}
	}
}

func (s *resubscribeSub) waitForError(sub Subscription) bool {
	defer sub.Unsubscribe()
	select {
	case err := <-sub.Err():
		s.lastSubErr = err
		return err == nil
	case <-s.unsub:
		return true
	}
}

func (s *resubscribeSub) backoffWait() bool {
	if time.Duration(mclock.Now()-s.lastTry) > s.backoffMax {
		s.waitTime = s.backoffMax / 10
	} else {
		s.waitTime *= 2
		if s.waitTime > s.backoffMax {
			s.waitTime = s.backoffMax
		}
	}

	t := time.NewTimer(s.waitTime)
	defer t.Stop()
	select {
	case <-t.C:
		return false
	case <-s.unsub:
		return true
	}
}

// SubscriptionScope provides a facility to unsubscribe multiple subscriptions at once.
//
// For code that handle more than one subscription, a scope can be used to conveniently
// unsubscribe all of them with a single call. The example demonstrates a typical use in a
// larger program.
//
// The zero value is ready to use.
type SubscriptionScope struct {
	mu     sync.Mutex
	subs   map[*scopeSub]struct{}
	closed bool
}

type scopeSub struct {
	sc *SubscriptionScope
	s  Subscription
}

// Track starts tracking a subscription. If the scope is closed, Track returns nil. The
// returned subscription is a wrapper. Unsubscribing the wrapper removes it from the
// scope.
func (sc *SubscriptionScope) Track(s Subscription) Subscription {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.closed {
		return nil
	}
	if sc.subs == nil {
		sc.subs = make(map[*scopeSub]struct{})
	}
	ss := &scopeSub{sc, s}
	sc.subs[ss] = struct{}{}
	return ss
}

// Close calls Unsubscribe on all tracked subscriptions and prevents further additions to
// the tracked set. Calls to Track after Close return nil.
func (sc *SubscriptionScope) Close() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.closed {
		return
	}
	sc.closed = true
	for s := range sc.subs {
		s.s.Unsubscribe()
	}
	sc.subs = nil
}

// Count returns the number of tracked subscriptions.
// It is meant to be used for debugging.
func (sc *SubscriptionScope) Count() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return len(sc.subs)
}

func (s *scopeSub) Unsubscribe() {
	s.s.Unsubscribe()
	s.sc.mu.Lock()
	defer s.sc.mu.Unlock()
	delete(s.sc.subs, s)
}

func (s *scopeSub) Err() <-chan error {
	return s.s.Err()
}
