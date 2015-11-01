package fake_doppler

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cloudfoundry/loggregatorlib/loggertesthelper"
	"github.com/cloudfoundry/loggregatorlib/server/handlers"
	gorilla "github.com/gorilla/websocket"
)

type FakeDoppler struct {
	ApiEndpoint                string
	connectionListener         net.Listener
	websocket                  *gorilla.Conn
	sendMessageChan            chan []byte
	TrafficControllerConnected chan *http.Request
	connectionPresent          bool
	sync.RWMutex
}

func New() *FakeDoppler {
	return &FakeDoppler{
		ApiEndpoint:                "127.0.0.1:1235",
		TrafficControllerConnected: make(chan *http.Request, 1),
		sendMessageChan:            make(chan []byte, 100),
	}
}

func (fakeDoppler *FakeDoppler) Start() error {
	var err error
	fakeDoppler.connectionListener, err = net.Listen("tcp", fakeDoppler.ApiEndpoint)
	s := &http.Server{Addr: fakeDoppler.ApiEndpoint, Handler: fakeDoppler}

	go func() {
		err = s.Serve(fakeDoppler.connectionListener)
	}()
	return err
}

func (fakeDoppler *FakeDoppler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	select {
	case fakeDoppler.TrafficControllerConnected <- request:
	default:
	}

	fakeDoppler.Lock()
	fakeDoppler.connectionPresent = true
	fakeDoppler.Unlock()

	handlers.NewWebsocketHandler(fakeDoppler.sendMessageChan, time.Millisecond*100, loggertesthelper.Logger()).ServeHTTP(writer, request)

	fakeDoppler.Lock()
	fakeDoppler.connectionPresent = false
	fakeDoppler.Unlock()
}

func (fakeDoppler *FakeDoppler) Stop() {
	fakeDoppler.connectionListener.Close()
}

func (fakeDoppler *FakeDoppler) ResetMessageChan() {
	fakeDoppler.sendMessageChan = make(chan []byte, 100)
}

func (fakeDoppler *FakeDoppler) ConnectionPresent() bool {
	fakeDoppler.Lock()
	defer fakeDoppler.Unlock()

	return fakeDoppler.connectionPresent
}

func (fakeDoppler *FakeDoppler) SendLogMessage(messageBody []byte) {
	fakeDoppler.sendMessageChan <- messageBody
}

func (fakeDoppler *FakeDoppler) CloseLogMessageStream() {
	close(fakeDoppler.sendMessageChan)
}
