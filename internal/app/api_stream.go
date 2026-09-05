/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"crypto/rand"
	"fmt"
	"time"
)

type StreamMessage struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

func streamMessageID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x-0000-4000-8000-%012x", time.Now().UnixNano(), time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func withStreamMessageID(message StreamMessage) StreamMessage {
	if message.ID == "" {
		message.ID = streamMessageID()
	}
	return message
}

func (a *App) Publish(message StreamMessage) error {
	return a.wss.BroadcastEvent(StreamEventID, withStreamMessageID(message))
}
