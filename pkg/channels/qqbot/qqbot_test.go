package qqbot

import (
	"testing"

	"github.com/tencent-connect/botgo/dto"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/media"
)

func TestAppendAttachmentTags(t *testing.T) {
	attachments := []*dto.MessageAttachment{
		{ContentType: "image/png"},
		{ContentType: "video/mp4"},
	}

	got := appendAttachmentTags("hello", []string{"ref1", "ref2"}, attachments)
	want := "hello [image: photo] [video]"
	if got != want {
		t.Fatalf("appendAttachmentTags() = %q, want %q", got, want)
	}
}

func TestAppendAttachmentTags_EmptyContent(t *testing.T) {
	attachments := []*dto.MessageAttachment{{ContentType: "image/jpeg"}}
	got := appendAttachmentTags("", []string{"ref1"}, attachments)
	if got != "[image: photo]" {
		t.Fatalf("appendAttachmentTags() = %q, want %q", got, "[image: photo]")
	}
}

func TestRememberGroupChat(t *testing.T) {
	ch := &QQChannel{
		groupChatIDs: make(map[string]bool),
	}
	if ch.isGroupChat("123") {
		t.Fatal("group should not be remembered initially")
	}
	ch.rememberGroupChat("123")
	if !ch.isGroupChat("123") {
		t.Fatal("group should be remembered")
	}
}

func TestResolveOutboundMedia_RemoteURL(t *testing.T) {
	ch := &QQChannel{}
	store := media.NewFileMediaStore()
	url, caption, err := ch.resolveOutboundMedia(bus.MediaPart{
		Ref:     "https://example.com/cat.png",
		Caption: "cat",
	}, store)
	if err != nil {
		t.Fatalf("resolveOutboundMedia() error = %v", err)
	}
	if url != "https://example.com/cat.png" {
		t.Fatalf("url = %q, want %q", url, "https://example.com/cat.png")
	}
	if caption != "cat" {
		t.Fatalf("caption = %q, want %q", caption, "cat")
	}
}

func TestResolveOutboundMedia_LocalRefFails(t *testing.T) {
	ch := &QQChannel{}
	store := media.NewFileMediaStore()
	if _, _, err := ch.resolveOutboundMedia(bus.MediaPart{Ref: "media://abc"}, store); err == nil {
		t.Fatal("resolveOutboundMedia() expected error for local media ref")
	}
}
