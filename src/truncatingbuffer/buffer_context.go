package truncatingbuffer

import (
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/cloudfoundry/dropsonde/envelope_extensions"
)

type BufferContext interface {
	EventAllowed(events.Envelope_EventType) bool
	Identifier() string
	DropsondeOrigin() string
	AppID(*events.Envelope) string
}

type DefaultContext struct {
	identifier string
	dropsondeOrigin string
}

func NewDefaultContext(origin string, identifier string) *DefaultContext{
	return &DefaultContext {
		identifier: identifier,
		dropsondeOrigin: origin,
	}
}

func(d *DefaultContext) EventAllowed(events.Envelope_EventType) bool{
	return true
}

func(d *DefaultContext) Identifier() string {
	return d.identifier
}

func (d *DefaultContext) DropsondeOrigin() string {
	return d.dropsondeOrigin
}

func (d *DefaultContext) AppID(envelope *events.Envelope) string {
	return envelope_extensions.GetAppId(envelope)
}

type LogAllowedContext struct {
	DefaultContext
}

func NewLogAllowedContext(origin string, identifier string) *LogAllowedContext {
	return &LogAllowedContext{
		DefaultContext {
			identifier: identifier,
			dropsondeOrigin: origin,
		},
	}
}

func (l *LogAllowedContext) EventAllowed(event events.Envelope_EventType) bool {
	return event == events.Envelope_LogMessage
}

type SystemContext struct {
	DefaultContext
}

func NewSystemContext(origin string, identifier string) *SystemContext {
	return &SystemContext{
		DefaultContext {
			identifier: identifier,
			dropsondeOrigin: origin,
		},
	}
}

func (s *SystemContext) AppID(*events.Envelope) string {
	return envelope_extensions.SystemAppId
}