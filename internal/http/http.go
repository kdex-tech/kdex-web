package http

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/text/language"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func GetParam(name string, defaultValue string, r *http.Request) string {
	value := r.PathValue(name)

	if value == "" {
		value = r.URL.Query().Get(name)
	}

	if value == "" {
		return defaultValue
	} else {
		return value
	}
}

func GetParamArray(name string, defaultValue []string, r *http.Request) []string {
	value := r.PathValue(name)
	var values []string

	if value == "" {
		values = r.URL.Query()[name]
	} else {
		values = []string{value}
	}

	if len(values) == 0 {
		return defaultValue
	} else {
		return values
	}
}

func GetLang(r *http.Request, defaultLanguage string, languages []language.Tag) (language.Tag, error) {
	log := logf.FromContext(r.Context())

	if len(languages) == 0 {
		return language.Und, errors.New("no supported languages provided")
	}

	l10n := GetParam("l10n", "", r)

	matcher := language.NewMatcher(languages)

	if l10n != "" {
		tag, err := language.Parse(l10n)
		if err != nil {
			return language.Und, fmt.Errorf("invalid language tag: %s", l10n)
		}

		_, index, confidence := matcher.Match(tag)
		if confidence == language.No {
			return language.Und, fmt.Errorf("language not supported: %s", l10n)
		}

		return languages[index], nil
	}

	preferredLanguages := []language.Tag{}

	acceptLanguages := strings.Split(r.Header.Get("Accept-Language"), ",")

	for _, acceptLanguage := range acceptLanguages {
		acceptLanguage = strings.TrimSpace(acceptLanguage)
		if acceptLanguage == "" {
			continue
		}
		prefix := strings.SplitN(acceptLanguage, ";", 2)[0]
		tag, err := language.Parse(prefix)
		if err != nil {
			log.V(1).Info("failed to parse accept-language header part, skipping", "part", acceptLanguage, "err", err)
			continue
		}
		preferredLanguages = append(preferredLanguages, tag)
	}

	_, index, _ := matcher.Match(preferredLanguages...)

	matchedTag := languages[index]

	if matchedTag.IsRoot() {
		// If we matched root but languages has something else, this might be a fallback.
		// If defaultLanguage is set, we return it.
		if defaultLanguage != "" {
			return language.Parse(defaultLanguage)
		}
		return language.Und, errors.New("no supported language found")
	}

	return matchedTag, nil
}
