package truncatingbuffer

import (
	"fmt"
	"sync"
	"time"

	"github.com/cloudfoundry/dropsonde/emitter"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gogo/protobuf/proto"
)

var lgrSource = proto.String("LGR")

type TruncatingBuffer struct {
	inputChannel        <-chan *events.Envelope
	context             BufferContext
	outputChannel       chan *events.Envelope
	logger              *gosteno.Logger
	lock                *sync.RWMutex
	droppedMessageCount uint64
	stopChannel         chan struct{}
}

func NewTruncatingBuffer(inputChannel <-chan *events.Envelope, bufferSize uint, context BufferContext, logger *gosteno.Logger, stopChannel chan struct{}) *TruncatingBuffer {
	if bufferSize < 3 {
		panic("bufferSize must be larger than 3 for overflow")
	}
	if context == nil {
		panic("context should not be nil")
	}
	outputChannel := make(chan *events.Envelope, bufferSize)
	return &TruncatingBuffer{
		inputChannel:        inputChannel,
		outputChannel:       outputChannel,
		logger:              logger,
		lock:                &sync.RWMutex{},
		droppedMessageCount: 0,
		stopChannel:         stopChannel,
		context: context,
	}
}

func (r *TruncatingBuffer) GetOutputChannel() <-chan *events.Envelope {
	r.lock.Lock()
	defer r.lock.Unlock()

	return r.outputChannel
}

func (r *TruncatingBuffer) closeOutputChannel() {
	close(r.outputChannel)
}

func (r *TruncatingBuffer) eventAllowed(eventType events.Envelope_EventType) bool {
	return r.context.EventAllowed(eventType)
}

func (r *TruncatingBuffer) Run() {
	defer r.closeOutputChannel()

	outputCapacity := cap(r.outputChannel)
	msgsSent := 0
	adjustDropped := 0
	totalDropped := uint64(0)
	r.lock.Lock()
	outputChannel := r.outputChannel
	r.lock.Unlock()

	for {
		select {
		case <-r.stopChannel:
			return
		case msg, ok := <-r.inputChannel:
			if !ok {
				return
			}
			if r.eventAllowed(msg.GetEventType()) {
				select {
				case outputChannel <- msg:
					if msgsSent < outputCapacity {
						msgsSent++
						if adjustDropped > 0 && (adjustDropped+msgsSent) > outputCapacity {
							adjustDropped--
						}
					}
				default:
					deltaDropped := uint64(len(outputChannel) - adjustDropped)
					totalDropped += uint64(deltaDropped)
					outputChannel = make(chan *events.Envelope, cap(outputChannel))
					appId := r.context.AppID(msg)

					r.notifyMessagesDropped(outputChannel, deltaDropped, totalDropped, appId)
					adjustDropped = len(outputChannel)
					outputChannel <- msg
					msgsSent = 1

					r.lock.Lock()
					r.outputChannel = outputChannel
					r.droppedMessageCount = totalDropped
					r.lock.Unlock()

					if r.logger != nil {
						r.logger.Warnd(map[string]interface{}{
							"dropped": deltaDropped, "total_dropped": totalDropped,
							"appId": appId, "sinkId": r.context.Identifier(),
						},
							"TB: Output channel too full")
					}
				}
			}
		}
	}
}

func (r *TruncatingBuffer) GetDroppedMessageCount() uint64 {
	r.lock.RLock()
	defer r.lock.RUnlock()
	messages := r.droppedMessageCount
	r.droppedMessageCount = 0
	return messages
}

func (r *TruncatingBuffer) notifyMessagesDropped(outputChannel chan *events.Envelope, deltaDropped, totalDropped uint64, appId string) {
	metrics.BatchAddCounter("TruncatingBuffer.totalDroppedMessages", deltaDropped)
	if r.eventAllowed(events.Envelope_LogMessage) {
		r.emitMessage(outputChannel, generateLogMessage(deltaDropped, totalDropped, appId, r.context.Identifier()))
	}
	if r.eventAllowed(events.Envelope_CounterEvent) {
		r.emitMessage(outputChannel, generateCounterEvent(deltaDropped, totalDropped))
	}
}

func (r *TruncatingBuffer) emitMessage(outputChannel chan *events.Envelope, event events.Event) {
	env, err := emitter.Wrap(event, r.context.DropsondeOrigin())
	if err == nil {
		outputChannel <- env
	} else {
		r.logger.Warnf("Error marshalling message: %v", err)
	}
}

func generateLogMessage(deltaDropped, totalDropped uint64, appId, sinkIdentifier string) *events.LogMessage {
	messageString := fmt.Sprintf("Log message output is too high. %d messages dropped (Total %d messages dropped) to %s.", deltaDropped, totalDropped, sinkIdentifier)

	messageType := events.LogMessage_ERR
	currentTime := time.Now()
	logMessage := &events.LogMessage{
		Message:     []byte(messageString),
		AppId:       &appId,
		MessageType: &messageType,
		SourceType:  lgrSource,
		Timestamp:   proto.Int64(currentTime.UnixNano()),
	}

	return logMessage
}

func generateCounterEvent(delta, total uint64) *events.CounterEvent {
	return &events.CounterEvent{
		Name:  proto.String("TruncatingBuffer.DroppedMessages"),
		Delta: proto.Uint64(delta),
		Total: proto.Uint64(total),
	}
}
