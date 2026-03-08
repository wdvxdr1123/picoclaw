package qqbot

import (
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("qqbot", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewQQBotChannel(cfg.Channels.QQBot, b)
	})
}
