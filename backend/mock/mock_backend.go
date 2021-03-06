package mock

import (
	"context"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/uw-labs/proximo/proto"
	"github.com/uw-labs/substrate"
)

// Backend is a simple mock backend implementation that allows one consumer or publisher at a time and
// allows user to set the messages to be consumed or check the messages that were produced.
type Backend struct {
	mutex    sync.Mutex
	messages map[string][]substrate.Message
}

// NewBackend returns a new instance of the mock backend.
func NewBackend() *Backend {
	return &Backend{
		messages: make(map[string][]substrate.Message),
	}
}

// GetTopic returns all messages published to a given topic.
func (b *Backend) GetTopic(topic string) []substrate.Message {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	return b.messages[topic]
}

// SetTopic sets messages to be consumed for a given topic.
func (b *Backend) SetTopic(topic string, messages []substrate.Message) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.messages[topic] = messages
}

func (b *Backend) NewAsyncSource(ctx context.Context, req *proto.StartConsumeRequest) (substrate.AsyncMessageSource, error) {
	return &mockSource{
		backend: b,
		config:  req,
	}, nil
}

type mockSource struct {
	backend *Backend
	config  *proto.StartConsumeRequest
}

func (source *mockSource) ConsumeMessages(ctx context.Context, messages chan<- substrate.Message, acks <-chan substrate.Message) error {
	source.backend.mutex.Lock()
	defer source.backend.mutex.Unlock()

	msgs, ok := source.backend.messages[source.config.GetTopic()]
	if !ok || len(msgs) == 0 {
		return nil
	}

	msgIndex := 0
	toAckIdx := 0

	processAck := func(ack substrate.Message) error {
		if toAckIdx == msgIndex {
			return status.Error(codes.InvalidArgument, "no acknowledgement expected")
		}
		if msgs[toAckIdx] != ack {
			return status.Error(codes.InvalidArgument, "wrong acknowledgement")
		}
		toAckIdx++
		return nil
	}

	for {
		if msgIndex < len(msgs) {
			select {
			case <-ctx.Done():
				return nil
			case messages <- msgs[msgIndex]:
				msgIndex++
			case ack := <-acks:
				if err := processAck(ack); err != nil {
					return err
				}
				if toAckIdx == len(msgs) {
					return nil
				}
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			case ack := <-acks:
				if err := processAck(ack); err != nil {
					return err
				}
				if toAckIdx == len(msgs) {
					return nil
				}
			}
		}
	}
}

func (source *mockSource) Close() error {
	return nil
}

func (source *mockSource) Status() (*substrate.Status, error) {
	panic("not implemented")
}

func (b *Backend) NewAsyncSink(ctx context.Context, req *proto.StartPublishRequest) (substrate.AsyncMessageSink, error) {
	return &mockSink{
		backend: b,
		config:  req,
	}, nil
}

type mockSink struct {
	backend *Backend
	config  *proto.StartPublishRequest
}

func (sink *mockSink) PublishMessages(ctx context.Context, acks chan<- substrate.Message, messages <-chan substrate.Message) error {
	sink.backend.mutex.Lock()
	defer sink.backend.mutex.Unlock()

	msgs, ok := sink.backend.messages[sink.config.GetTopic()]
	if !ok {
		msgs = make([]substrate.Message, 0)
	}
	defer func() {
		sink.backend.messages[sink.config.GetTopic()] = msgs
	}()

	toConfirm := make([]substrate.Message, 0)
	for {
		if len(toConfirm) == 0 {
			select {
			case <-ctx.Done():
				return nil
			case msg := <-messages:
				msgs = append(msgs, msg)
				toConfirm = append(toConfirm, msg)
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			case msg := <-messages:
				msgs = append(msgs, msg)
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
