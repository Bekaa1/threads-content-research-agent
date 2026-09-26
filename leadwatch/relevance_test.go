package leadwatch

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

func TestTopicGateRejectsObservedUnrelatedPosts(t *testing.T) {
	for _, text := range []string{
		"毎朝2時間以上あさんぽしてるんですけど、カエルがいました",
		"Open ChatGPT, send it this image, and type: Fill it with things that represent me",
		"IM HOLDING MY BREATHHHH", ". #fyp #viral #relatable",
		"看到这个我连生气的力气都没有了", "I love you, Keanu", "Kaisi ho Fatima?",
	} {
		for _, query := range []string{"need a web developer", "нужен сайт", "ищу интегратора amoCRM"} {
			if queryTopicMatches(query, text) {
				t.Fatalf("query %q accepted %q", query, text)
			}
		}
	}
}

func TestTopicGateAcceptsAliasesWithoutExactQueryPhrase(t *testing.T) {
	for _, pair := range [][2]string{
		{"нужен сайт", "Посоветуйте мастера по лендингам"},
		{"need a web developer", "Can anyone recommend a programmer?"},
		{"ищу интегратора amoCRM", "Кто поможет настроить Kommo?"},
		{"looking for CRM automation", "Our n8n workflows keep failing"},
		{"need someone to build an app", "Нужна помощь с мобильным приложением"},
		{"need a web developer", "I sell website development services"},
		{"React Native", "Looking for a React Native contractor"},
	} {
		if !queryTopicMatches(pair[0], pair[1]) {
			t.Fatalf("rejected topic match %+v", pair)
		}
	}
	if queryTopicMatches("need someone", "I need someone to bring coffee") {
		t.Fatal("intent alone is not a service topic")
	}
}

func TestWorkerRejectsUnrelatedPostsInsideSearchConnection(t *testing.T) {
	var logs strings.Builder
	store := newMemoryStore()
	worker, _ := NewWorker(fakeSearcher{posts: map[string][]threads.SearchResult{"нужен сайт": {{ID: "noise", Text: "I love you, Keanu", Source: threads.SearchSourceSSR}}}}, fakeAnalyzer{}, &fakeNotifier{}, store, []string{"нужен сайт"}, "", slog.New(slog.NewJSONHandler(&logs, nil)))
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("off-topic response counted as success")
	}
	if len(store.posts) != 0 || len(store.assessments) != 0 {
		t.Fatal("unrelated post reached DB or model")
	}
	if !strings.Contains(logs.String(), `"status":"off_topic"`) {
		t.Fatal(logs.String())
	}
}
