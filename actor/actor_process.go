package actor

import (
	"sync/atomic"

	"github.com/qjpcpu/protoactor-go/mailbox"
)

type ActorProcess struct {
	mailbox mailbox.Mailbox
	dead    int32
}

var _ Process = &ActorProcess{}

func NewActorProcess(mailbox mailbox.Mailbox) *ActorProcess {
	return &ActorProcess{mailbox: mailbox}
}

func (ref *ActorProcess) SendUserMessage(pid *PID, message interface{}) {
	ref.mailbox.PostUserMessage(message)
}
func (ref *ActorProcess) SendSystemMessage(pid *PID, message interface{}) {
	ref.mailbox.PostSystemMessage(message)
}

func (ref *ActorProcess) Stop(pid *PID) {
	atomic.StoreInt32(&ref.dead, 1)
	ref.SendSystemMessage(pid, stopMessage)
}
