package model

import (
	"context"
	"time"
)

type Source struct {
	Key      string
	Harness  string
	Path     string
	Version  string
	NativeID string
	Metadata map[string]string
}

type Session struct {
	Key        string
	Harness    string
	NativeID   string
	ParentID   string
	Name       string
	CWD        string
	SourceKey  string
	SourcePath string
	StartedAt  time.Time
	UpdatedAt  time.Time
	GitBranch  string
	GitRemote  string
}

type Entry struct {
	Key        string
	SessionKey string
	NativeID   string
	ParentID   string
	Role       string
	Kind       string
	Timestamp  time.Time
	Text       string
}

type Chunk struct {
	Key           string
	SessionKey    string
	EntryKey      string
	EntryNativeID string
	EntryParentID string
	Kind          string
	Part          int
	Role          string
	Timestamp     time.Time
	Text          string
	TextHash      string
	Grams         string
}

type ParsedSource struct {
	Sessions []Session
	Entries  []Entry
	Warnings []string
}

type Adapter interface {
	Name() string
	Discover(context.Context) ([]Source, error)
	Parse(context.Context, Source) (ParsedSource, error)
}

type AdapterCloser interface {
	Close() error
}
