package task

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxTopicLen is the longest topic, in characters (ADR todo-0024).
const MaxTopicLen = 20

// NormalizeTopic trims a topic and checks it. Empty means "no topic".
func NormalizeTopic(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "\r\n") {
		return "", fmt.Errorf("話題に改行は使えない")
	}
	if n := utf8.RuneCountInString(s); n > MaxTopicLen {
		return "", fmt.Errorf("話題は %d 文字まで（%d 文字）", MaxTopicLen, n)
	}
	return s, nil
}

// SetTopic sets or clears (topic "") the task's topic.
func (s *Store) SetTopic(id, topic string, actor Actor) (Task, error) {
	topic, err := NormalizeTopic(topic)
	if err != nil {
		return Task{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	from := t.Topic
	if from == topic {
		return t, nil
	}
	t.Topic = topic
	s.st.Tasks[i] = t

	if err := s.record(Event{TaskID: id, Kind: KindTopicSet, Actor: actor, FromValue: from, ToValue: topic}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}
