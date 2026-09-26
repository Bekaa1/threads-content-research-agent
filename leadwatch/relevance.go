package leadwatch

import (
	"strings"
	"unicode"
)

// This is only a cheap topic sanity check, not a buyer-intent classifier.
// Anonymous Threads can return an unrelated feed inside a searchResults object.
// Match service terms and aliases, not an exact phrase or generic "need/looking".
var serviceTopicAliases = [][]string{
	{"сайт", "лендинг", "веб", "верст", "вёрст", "website", "web", "site", "landing", "wordpress", "shopify"},
	{"разработ", "программ", "developer", "programmer", "coder", "coding", "software", "engineer"},
	{"crm", "срм", "амосрм", "amocrm", "kommo", "коммо", "битрикс", "bitrix", "воронк"},
	{"автоматиз", "автоматизац", "automation", "automate", "automating", "workflow", "n8n"},
	{"интегратор", "интеграц", "integrator", "integration", "integrate", "integrating", "api"},
	{"бот", "бота", "ботов", "чатбот", "bot", "bots", "chatbot", "assistant", "ассистент"},
	{"приложен", "мобильн", "app", "apps", "mobile", "reactnative"},
}

func topicWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func containsTopic(words, aliases []string) bool {
	for _, word := range words {
		for _, alias := range aliases {
			if word == alias || (len([]rune(alias)) >= 4 && strings.HasPrefix(word, alias)) {
				return true
			}
		}
	}
	return false
}

func queryTopicMatches(query, text string) bool {
	queryWords, postWords := topicWords(query), topicWords(text)
	knownTopic := false
	for _, aliases := range serviceTopicAliases {
		if containsTopic(queryWords, aliases) {
			knownTopic = true
			if containsTopic(postWords, aliases) {
				return true
			}
		}
	}
	if knownTopic {
		return false
	}
	// Custom queries still need a substantive lexical anchor. Short fragments
	// and intent-only queries are too broad to validate an anonymous response.
	stop := map[string]bool{"нужен": true, "нужна": true, "нужно": true, "нужны": true, "ищу": true, "ищем": true, "требуется": true, "помогите": true, "посоветуйте": true, "сделать": true, "looking": true, "need": true, "want": true, "someone": true, "build": true, "help": true, "recommend": true, "please": true, "hire": true, "hiring": true, "find": true, "with": true, "that": true}
	for _, word := range queryWords {
		if len([]rune(word)) >= 4 && !stop[word] && containsTopic(postWords, []string{word}) {
			return true
		}
	}
	return false
}

// MatchesQueryTopic applies the same sanity check to trusted local browser input.
func MatchesQueryTopic(query, text string) bool { return queryTopicMatches(query, text) }
