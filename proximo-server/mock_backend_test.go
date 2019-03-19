package main

import (
	"context"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/pkg/errors"
	"github.com/uw-labs/proximo/proto"
	"github.com/uw-labs/substrate"
)

// MockBackend is a simple backend implementation that allows one consumer or publisher at a time and
// allows user to set the messages to be consumed or check the messages that were produced.
type MockBackend struct {
	mutex    sync.Mutex
	messages map[string][]*proto.Message
}

// NewMockBackend returns a new instance of the mock backend.
func NewMockBackend() *MockBackend {
	return &MockBackend{
		messages: make(map[string][]*proto.Message),
	}
}

// GetTopic returns all messages published to a given topic.
func (b *MockBackend) GetTopic(topic string) []*proto.Message {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	return b.messages[topic]
}

// SetTopic sets messages to be consumed for a given topic.
func (b *MockBackend) SetTopic(topic string, messages []*proto.Message) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.messages[topic] = messages
}

func (b *MockBackend) HandleConsume(ctx context.Context, conf consumerConfig, forClient chan<- *proto.Message, confirmRequest <-chan *proto.Confirmation) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	messages, ok := b.messages[conf.topic]
	if !ok || len(messages) == 0 {
		return nil
	}

	msgIndex := 0
	toAckIdx := 0

	processConfirm := func(confirm *proto.Confirmation) error {
		if toAckIdx == msgIndex {
			return status.Error(codes.InvalidArgument, "no acknowledgement expected")
		}
		if messages[toAckIdx].Id != confirm.MsgID {
			return status.Error(codes.InvalidArgument, "wrong acknowledgement")
		}
		toAckIdx++
		return nil
	}

	for {
		if msgIndex < len(messages) {
			select {
			case <-ctx.Done():
				return nil
			case forClient <- messages[msgIndex]:
				msgIndex++
			case confirm := <-confirmRequest:
				if err := processConfirm(confirm); err != nil {
					return err
				}
				if toAckIdx == len(messages) {
					return nil
				}
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			case confirm := <-confirmRequest:
				if err := processConfirm(confirm); err != nil {
					return err
				}
				if toAckIdx == len(messages) {
					return nil
				}
			}
		}
	}
}

func (b *MockBackend) NewAsyncSink(ctx context.Context, config SinkConfig) (substrate.AsyncMessageSink, error) {
	return &mockSink{
		backend: b,
		config:  config,
	}, nil
}

type mockSink struct {
	backend *MockBackend
	config  SinkConfig
}

func (sink *mockSink) PublishMessages(ctx context.Context, acks chan<- substrate.Message, messages <-chan substrate.Message) error {
	sink.backend.mutex.Lock()
	defer sink.backend.mutex.Unlock()

	msgs, ok := sink.backend.messages[sink.config.Topic]
	if !ok {
		msgs = make([]*proto.Message, 0)
	}
	defer func() {
		sink.backend.messages[sink.config.Topic] = msgs
	}()

	toConfirm := make([]substrate.Message, 0)
	for {
		if len(toConfirm) == 0 {
			select {
			case <-ctx.Done():
				return nil
			case msg := <-messages:
				pMsg, ok := msg.(proximoMsg)
				if !ok {
					return errors.Errorf("unexpected message: %v", msg)
				}
				msgs = append(msgs, pMsg.msg)
				toConfirm = append(toConfirm, msg)
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			case msg := <-messages:
				pMsg, ok := msg.(proximoMsg)
				if !ok {
					return errors.Errorf("unexpected message: %v", msg)
				}
				msgs = append(msgs, pMsg.msg)
				toConfirm = append(toConfirm, msg)
			case acks <- toConfirm[0]:
				toConfirm = toConfirm[1:]
			}
		}
	}
}

func (sink *mockSink) Close() error {
	return nil
}

func (sink *mockSink) Status() (*substrate.Status, error) {
	panic("not implemented")
}
