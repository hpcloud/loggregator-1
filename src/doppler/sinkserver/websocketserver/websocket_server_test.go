package websocketserver_test

import (
	"doppler/sinkserver/blacklist"
	"doppler/sinkserver/sinkmanager"
	"doppler/sinkserver/websocketserver"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
	"io/ioutil"

	"github.com/cloudfoundry/dropsonde/factories"
	"github.com/cloudfoundry/loggregatorlib/loggertesthelper"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gorilla/websocket"

	"github.com/cloudfoundry/dropsonde/emitter"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
)

var _ = Describe("WebsocketServer", func() {

	var server *websocketserver.WebsocketServer
	var sinkManager = sinkmanager.New(1024, false, blacklist.New(nil), loggertesthelper.Logger(), 100, "dropsonde-origin", 1*time.Second, 0, 1*time.Second, 500*time.Millisecond)
	var appId = "my-app"
	var wsReceivedChan chan []byte
	var apiEndpoint string

	BeforeEach(func() {
		logger := loggertesthelper.Logger()
		wsReceivedChan = make(chan []byte)

		apiEndpoint = net.JoinHostPort("127.0.0.1", strconv.Itoa(9091+config.GinkgoConfig.ParallelNode*10))
		var err error
		server, err = websocketserver.New(apiEndpoint, sinkManager, 100*time.Millisecond, 100*time.Millisecond, 100, "dropsonde-origin", logger)
		Expect(err).NotTo(HaveOccurred())
		go server.Start()
		serverUrl := fmt.Sprintf("ws://%s/apps/%s/stream", apiEndpoint, appId)
		websocket.DefaultDialer = &websocket.Dialer{HandshakeTimeout: 10 * time.Millisecond}
		Eventually(func() error { _, _, err := websocket.DefaultDialer.Dial(serverUrl, http.Header{}); return err }, 1).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		server.Stop()
		time.Sleep(time.Millisecond * 10)
	})

	Describe("failed connections", func() {
		It("fails without an appId", func() {
			_, _, err := AddWSSink(wsReceivedChan, fmt.Sprintf("ws://%s/apps//stream", apiEndpoint))
			Expect(err).To(HaveOccurred())
		})

		It("fails with bad path", func() {
			_, _, err := AddWSSink(wsReceivedChan, fmt.Sprintf("ws://%s/apps/my-app/junk", apiEndpoint))
			Expect(err).To(HaveOccurred())
		})
	})

	It("dumps buffer data to the websocket client with /recentlogs", func() {
		lm, _ := emitter.Wrap(factories.NewLogMessage(events.LogMessage_OUT, "my message", appId, "App"), "origin")
		sinkManager.SendTo(appId, lm)

		_, _, err := AddWSSink(wsReceivedChan, fmt.Sprintf("ws://%s/apps/%s/recentlogs", apiEndpoint, appId))
		Expect(err).NotTo(HaveOccurred())

		rlm, err := receiveEnvelope(wsReceivedChan)
		Expect(err).NotTo(HaveOccurred())
		Expect(rlm.GetLogMessage().GetMessage()).To(Equal(lm.GetLogMessage().GetMessage()))
	})

	It("dumps container metric data to the websocket client with /containermetrics", func() {
		cm := factories.NewContainerMetric(appId, 0, 42.42, 1234, 123412341234)
		envelope, _ := emitter.Wrap(cm, "origin")
		sinkManager.SendTo(appId, envelope)

		_, _, err := AddWSSink(wsReceivedChan, fmt.Sprintf("ws://%s/apps/%s/containermetrics", apiEndpoint, appId))
		Expect(err).NotTo(HaveOccurred())

		rcm, err := receiveEnvelope(wsReceivedChan)
		Expect(err).NotTo(HaveOccurred())
		Expect(rcm.GetContainerMetric()).To(Equal(cm))
	})

	It("skips sending data to the websocket client with a marshal error", func() {
		cm := factories.NewContainerMetric(appId, 0, 42.42, 1234, 123412341234)
		cm.InstanceIndex = nil
		envelope, _ := emitter.Wrap(cm, "origin")
		sinkManager.SendTo(appId, envelope)

		_, _, err := AddWSSink(wsReceivedChan, fmt.Sprintf("ws://%s/apps/%s/containermetrics", apiEndpoint, appId))
		Expect(err).NotTo(HaveOccurred())
		Consistently(wsReceivedChan).ShouldNot(Receive())
	})

	It("sends data to the websocket client with /stream", func() {
		stopKeepAlive, _, err := AddWSSink(wsReceivedChan, fmt.Sprintf("ws://%s/apps/%s/stream", apiEndpoint, appId))
		Expect(err).NotTo(HaveOccurred())
		lm, _ := emitter.Wrap(factories.NewLogMessage(events.LogMessage_OUT, "my message", appId, "App"), "origin")
		sinkManager.SendTo(appId, lm)

		rlm, err := receiveEnvelope(wsReceivedChan)
		Expect(err).NotTo(HaveOccurred())
		Expect(rlm.GetLogMessage().GetMessage()).To(Equal(lm.GetLogMessage().GetMessage()))
		close(stopKeepAlive)
	})

	It("sends data to the websocket firehose client", func() {
		stopKeepAlive, _, err := AddWSSink(wsReceivedChan, fmt.Sprintf("ws://%s/firehose/fire-subscription-a", apiEndpoint))
		Expect(err).NotTo(HaveOccurred())
		lm, _ := emitter.Wrap(factories.NewLogMessage(events.LogMessage_OUT, "my message", appId, "App"), "origin")
		sinkManager.SendTo(appId, lm)

		rlm, err := receiveEnvelope(wsReceivedChan)
		Expect(err).NotTo(HaveOccurred())
		Expect(rlm.GetLogMessage().GetMessage()).To(Equal(lm.GetLogMessage().GetMessage()))
		close(stopKeepAlive)
	})

	It("sends each message to only one of many firehoses with the same subscription id", func() {
		firehoseAChan1 := make(chan []byte, 100)
		stopKeepAlive1, _, err := AddWSSink(firehoseAChan1, fmt.Sprintf("ws://%s/firehose/fire-subscription-x", apiEndpoint))
		Expect(err).NotTo(HaveOccurred())

		firehoseAChan2 := make(chan []byte, 100)
		stopKeepAlive2, _, err := AddWSSink(firehoseAChan2, fmt.Sprintf("ws://%s/firehose/fire-subscription-x", apiEndpoint))
		Expect(err).NotTo(HaveOccurred())

		lm, _ := emitter.Wrap(factories.NewLogMessage(events.LogMessage_OUT, "my message", appId, "App"), "origin")

		for i := 0; i < 2; i++ {
			sinkManager.SendTo(appId, lm)
		}

		Eventually(func() int {
			return len(firehoseAChan1) + len(firehoseAChan2)
		}).Should(Equal(2))

		Consistently(func() int {
			return len(firehoseAChan2)
		}).Should(BeNumerically(">", 0))

		Consistently(func() int {
			return len(firehoseAChan1)
		}).Should(BeNumerically(">", 0))

		close(stopKeepAlive1)
		close(stopKeepAlive2)
	}, 2)

	It("works with malformed firehose path", func() {
		resp, err := http.Get(fmt.Sprintf("http://%s/firehose", apiEndpoint))
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		bytes, err := ioutil.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(bytes).To(ContainSubstring("missing subscription id in firehose request"))
	})

	It("still sends to 'live' sinks", func() {
		stopKeepAlive, connectionDropped, err := AddWSSink(wsReceivedChan, fmt.Sprintf("ws://%s/apps/%s/stream", apiEndpoint, appId))
		Expect(err).NotTo(HaveOccurred())
		Consistently(connectionDropped, 0.2).ShouldNot(BeClosed())

		lm, _ := emitter.Wrap(factories.NewLogMessage(events.LogMessage_OUT, "my message", appId, "App"), "origin")
		sinkManager.SendTo(appId, lm)

		rlm, err := receiveEnvelope(wsReceivedChan)
		Expect(err).NotTo(HaveOccurred())
		Expect(rlm).ToNot(BeNil())
		close(stopKeepAlive)
	})

	It("closes the client when the keep-alive stops", func() {
		stopKeepAlive, connectionDropped, err := AddWSSink(wsReceivedChan, fmt.Sprintf("ws://%s/apps/%s/stream", apiEndpoint, appId))
		Expect(err).NotTo(HaveOccurred())
		Expect(stopKeepAlive).ToNot(Receive())
		close(stopKeepAlive)
		Eventually(connectionDropped).Should(BeClosed())
	})

	It("times out slow connections", func() {
		errChan := make(chan error)
		url := fmt.Sprintf("ws://%s/apps/%s/stream", apiEndpoint, appId)
		AddSlowWSSink(wsReceivedChan, errChan, 2*time.Second, url)
		var err error
		Eventually(errChan, 5).Should(Receive(&err))
		Expect(err).To(HaveOccurred())
	})
})

func receiveEnvelope(dataChan <-chan []byte) (*events.Envelope, error) {
	var data []byte
	Eventually(dataChan).Should(Receive(&data))
	return parseEnvelope(data)
}
