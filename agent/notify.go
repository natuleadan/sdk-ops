package main

import (
	"log"

	"github.com/natuleadan/sdk-ops/notify"
)

type notifyDispatcher struct {
	notifiers []notify.Notifier
}

func newNotifyDispatcher(cfg AgentConfig) *notifyDispatcher {
	nc := cfg.toNotifyConfig()
	nn := notify.BuildNotifiers(nc)
	if len(nn) == 0 {
		log.Println("notify: no notifiers configured (set SDK_OPS_SLACK_WEBHOOK, etc.)")
	}
	return &notifyDispatcher{notifiers: nn}
}

func (d *notifyDispatcher) send(title, message string) {
	if len(d.notifiers) == 0 {
		return
	}
	errs := notify.SendAll(d.notifiers, title, message)
	for _, err := range errs {
		log.Printf("notify: %v", err)
	}
	if len(errs) == 0 {
		log.Printf("notify: sent to %d notifiers", len(d.notifiers))
	}
}
