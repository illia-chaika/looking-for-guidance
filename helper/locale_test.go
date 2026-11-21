package main

import (
	"regexp"
	"slices"
	"testing"
)

// The tests read the menu in Russian whatever language the machine is set to.
func init() { menuLanguage = func() string { return "ru" } }

func TestMenuTextsAreCompleteInEveryLanguage(t *testing.T) {
	verbs := regexp.MustCompile(`%[sd]`)
	for english, translations := range menuTexts {
		want := verbs.FindAllString(english, -1)
		slices.Sort(want)
		for i, text := range translations {
			got := verbs.FindAllString(text, -1)
			slices.Sort(got)
			if text == "" || !slices.Equal(got, want) {
				t.Errorf("%q in %s is %q", english, menuLanguages[i], text)
			}
		}
	}
}

func TestMenuLanguageFollowsTheFirstPreferenceItHas(t *testing.T) {
	for _, tc := range []struct {
		preferred []string
		want      string
	}{
		{[]string{"en-US", "ru-UA", "uk-UA"}, "en"},
		{[]string{"uk-UA", "ru-UA", "en-US"}, "uk"},
		{[]string{"be-BY", "ru-UA", "en-US"}, "ru"},
		{[]string{"it-IT"}, "it"},
		{[]string{"ru_RU.UTF-8"}, "ru"},
		{[]string{"pt-BR"}, "pt"},
		{[]string{"zh-Hant-TW"}, "zh-Hant"},
		{[]string{"zh-HK"}, "zh-Hant"},
		{[]string{"zh-Hans-CN"}, "zh-Hans"},
		{[]string{"zh-CN"}, "zh-Hans"},
		{[]string{"ja-JP"}, "en"},
		{nil, "en"},
	} {
		if got := matchLanguage(tc.preferred); got != tc.want {
			t.Errorf("%v gives %s, want %s", tc.preferred, got, tc.want)
		}
	}
	if got := trIn("ru", "Chats: %d", 3); got != "Чатов: 3" {
		t.Errorf("Russian menu text: %q", got)
	}
	if got := trIn("en", "Chats: %d", 3); got != "Chats: 3" {
		t.Errorf("English menu text: %q", got)
	}
	if got := trIn("de", "A text nobody translated"); got != "A text nobody translated" {
		t.Errorf("an unknown text must pass through: %q", got)
	}
}
