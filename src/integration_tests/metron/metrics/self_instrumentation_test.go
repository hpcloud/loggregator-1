package metrics_test

import (
	"net"
	"time"

	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gogo/protobuf/proto"

	. "matchers"

	dopplerconfig "doppler/config"
	"doppler/dopplerservice"
	"integration_tests/metron/metrics"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Self Instrumentation", func() {
	var (
		testDoppler  *metrics.TestDoppler
		stopAnnounce chan chan bool
		metronInput  net.Conn
	)

	BeforeEach(func() {
		testDoppler = metrics.NewTestDoppler(metronRunner.DropsondeAddress())
		go testDoppler.Start()

		dopplerConfig := &dopplerconfig.Config{
			Index:   0,
			JobName: "job",
			Zone:    "z9",
			DropsondeIncomingMessagesPort: uint32(metronRunner.DropsondePort),
		}

		stopAnnounce = dopplerservice.Announce("127.0.0.1", time.Minute, dopplerConfig, etcdAdapter, gosteno.NewLogger("test"))

		metronRunner.Protocol = "tls"
		metronRunner.Start()

		env := basicValueMessageEnvelope()
		env.Origin = proto.String("foobar")
		bytes, err := proto.Marshal(env)
		Expect(err).ToNot(HaveOccurred())

		metronInput, _ = net.Dial("udp4", metronRunner.MetronAddress())
		Eventually(func() bool {
			metronInput.Write(bytes)
			select {
			case <-testDoppler.MessageChan:
				return true
			default:
			}
			return false
		}, 2).Should(BeTrue())
	})

	AfterEach(func() {
		metronInput.Close()
		testDoppler.Stop()
		close(stopAnnounce)
		metronRunner.Stop()
	})

	drainAndEval := func(ch chan *events.Envelope, f func(*events.Envelope) bool) bool {
		for {
			select {
			case env := <-ch:
				b := f(env)
				if b {
					return true
				}
			default:
				return false
			}
		}
	}

	waitForEvent := func(sendBytes []byte, match func(expected, actual *events.Envelope) bool, expected *events.Envelope) {
		Eventually(func() bool {
			if sendBytes != nil {
				metronInput.Write(sendBytes)
			}

			return drainAndEval(testDoppler.MessageChan, func(env *events.Envelope) bool {
				return match(expected, env)
			})
		}, 3, 100*time.Millisecond).Should(BeTrue())
	}

	eventNeverOccurs := func(sendBytes []byte, match func(expected, actual *events.Envelope) bool, expected *events.Envelope) {
		Consistently(func() bool {
			if sendBytes != nil {
				metronInput.Write(sendBytes)
			}

			return drainAndEval(testDoppler.MessageChan, func(env *events.Envelope) bool {
				return match(expected, env)
			})
		}, 1, 100*time.Millisecond).Should(BeFalse())
	}

	It("sends metrics about the Dropsonde network reader", func() {
		metronInput.Write(basicValueMessage())

		expected := events.Envelope{
			Origin:    proto.String("MetronAgent"),
			EventType: events.Envelope_CounterEvent.Enum(),
			CounterEvent: &events.CounterEvent{
				Name:  proto.String("dropsondeAgentListener.receivedMessageCount"),
				Delta: proto.Uint64(1),
				Total: proto.Uint64(1),
			},
		}

		waitForEvent(basicValueMessage(), matchCounter, &expected)
	})

	Describe("for Message Aggregator", func() {
		It("emits metrics for counter events", func() {
			metronInput.Write(basicCounterEvent())
			metronInput.Write(basicCounterEvent())

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("MessageAggregator.counterEventReceived"),
					Delta: proto.Uint64(2),
					Total: proto.Uint64(2),
				},
			}

			matcher := MatchSpecifiedContents(&expected)
			Eventually(func() bool {
				return drainAndEval(testDoppler.MessageChan, func(env *events.Envelope) bool {
					b, _ := matcher.Match(env)
					return b
				})
			}, 2).Should(BeTrue())

			expected = events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("MessageAggregator.counterEventReceived"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(3),
				},
			}

			Consistently(func() bool {
				return drainAndEval(testDoppler.MessageChan, func(env *events.Envelope) bool {
					return env.EventType == expected.EventType && env.GetCounterEvent().Name == expected.GetCounterEvent().Name
				})
			}, 2).Should(BeFalse())
		})

		It("emits metrics for http start", func() {
			metronInput.Write(basicHTTPStartEvent())

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("MessageAggregator.httpStartReceived"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(1),
				},
			}

			waitForEvent(basicHTTPStartEvent(), matchCounter, &expected)
		})

		It("emits metrics for http stop", func() {
			metronInput.Write(basicHTTPStopEvent())

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("MessageAggregator.httpStopReceived"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(1),
				},
			}

			waitForEvent(basicHTTPStopEvent(), matchCounter, &expected)
		})

		It("emits metrics for unmatched http stop", func() {
			metronInput.Write(basicHTTPStopEvent())

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("MessageAggregator.httpUnmatchedStopReceived"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(1),
				},
			}

			waitForEvent(basicHTTPStopEvent(), matchCounter, &expected)
		})

		It("emits metrics for http start stop", func() {
			metronInput.Write(basicHTTPStartEvent())
			metronInput.Write(basicHTTPStopEvent())

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("MessageAggregator.httpStartStopEmitted"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(1),
				},
			}

			matcher := MatchSpecifiedContents(&expected)
			Eventually(func() bool {
				return drainAndEval(testDoppler.MessageChan, func(env *events.Envelope) bool {
					b, _ := matcher.Match(env)
					return b
				})
			}, 2).Should(BeTrue())
		})

		It("emits metrics for uncategorized events", func() {
			message := basicValueMessageEnvelope()
			message.EventType = events.Envelope_LogMessage.Enum()
			bytes, err := proto.Marshal(message)
			Expect(err).ToNot(HaveOccurred())

			metronInput.Write(bytes)

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("MessageAggregator.uncategorizedEvents"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(1),
				},
			}

			waitForEvent(bytes, matchCounter, &expected)
		})
	})

	Describe("for Dropsonde unmarshaller", func() {
		It("counts errors", func() {
			metronInput.Write([]byte{1, 2, 3})

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("dropsondeUnmarshaller.unmarshalErrors"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(1),
				},
			}

			waitForEvent([]byte{1, 2, 3}, matchCounter, &expected)
		})

		It("counts unmarshalled Dropsonde messages by type", func() {
			metronInput.Write(basicValueMessage())

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("dropsondeUnmarshaller.valueMetricReceived"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(1),
				},
			}

			waitForEvent(basicValueMessage(), matchCounter, &expected)
		})

		It("counts log messages specially", func() {
			logEnvelope := &events.Envelope{
				Origin:    proto.String("fake-origin-2"),
				EventType: events.Envelope_LogMessage.Enum(),
				LogMessage: &events.LogMessage{
					Message:     []byte("hello"),
					MessageType: events.LogMessage_OUT.Enum(),
					Timestamp:   proto.Int64(1234),
				},
			}
			logBytes, _ := proto.Marshal(logEnvelope)

			metronInput.Write(logBytes)

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("dropsondeUnmarshaller.logMessageTotal"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(1),
				},
			}

			waitForEvent(logBytes, matchCounter, &expected)
		})

		It("counts unknown event types", func() {
			message := basicValueMessageEnvelope()
			message.EventType = events.Envelope_EventType(2000).Enum()
			bytes, err := proto.Marshal(message)
			Expect(err).ToNot(HaveOccurred())

			_, err = metronInput.Write(bytes)
			Expect(err).NotTo(HaveOccurred())

			message = basicValueMessageEnvelope()
			badEventType := events.Envelope_EventType(1000)
			message.EventType = &badEventType
			bytes, err = proto.Marshal(message)
			Expect(err).ToNot(HaveOccurred())

			_, err = metronInput.Write(bytes)
			Expect(err).NotTo(HaveOccurred())

			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("dropsondeUnmarshaller.unknownEventTypeReceived"),
					Delta: proto.Uint64(2),
					Total: proto.Uint64(2),
				},
			}

			waitForEvent(bytes, matchCounter, &expected)
		})

		It("does not forward unknown events", func() {
			message := basicValueMessageEnvelope()
			message.EventType = events.Envelope_EventType(2000).Enum()
			bytes, err := proto.Marshal(message)
			Expect(err).ToNot(HaveOccurred())

			metronInput.Write(bytes)

			matcher := MatchSpecifiedContents(&message)
			Eventually(func() bool {
				return drainAndEval(testDoppler.MessageChan, func(env *events.Envelope) bool {
					b, _ := matcher.Match(env)
					return b
				})
			}).Should(BeFalse())

			message = basicValueMessageEnvelope()
			badEventType := events.Envelope_EventType(1000)
			message.EventType = &badEventType
			bytes, err = proto.Marshal(message)
			Expect(err).ToNot(HaveOccurred())

			metronInput.Write(bytes)

			eventNeverOccurs(bytes, matchCounter, message)
		})
	})

	Describe("for Dropsonde marshaller", func() {
		It("counts marshalled Dropsonde messages by type", func() {
			expected := events.Envelope{
				Origin:    proto.String("MetronAgent"),
				EventType: events.Envelope_CounterEvent.Enum(),
				CounterEvent: &events.CounterEvent{
					Name:  proto.String("dropsondeMarshaller.valueMetricMarshalled"),
					Delta: proto.Uint64(1),
					Total: proto.Uint64(1),
				},
			}

			waitForEvent(basicValueMessage(), matchCounter, &expected)
		})
	})
})

func matchCounter(expected, actual *events.Envelope) bool {
	return expected.GetOrigin() == actual.GetOrigin() &&
		expected.GetEventType() == actual.GetEventType() &&
		expected.GetCounterEvent().GetName() == actual.GetCounterEvent().GetName() &&
		actual.GetCounterEvent().GetTotal() > 0
}
