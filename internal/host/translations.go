package host

import (
	"fmt"

	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

func NewTranslations(defaultLanguage string, translations map[string]kdexv1alpha1.KDexTranslationSpec) (*Translations, error) {
	catalogBuilder := catalog.NewBuilder()

	if err := catalogBuilder.SetString(language.Make(defaultLanguage), "_", "_"); err != nil {
		return nil, fmt.Errorf("failed to set default translation %s %s", defaultLanguage, "_")
	}

	keys := []string{}
	for name, translation := range translations {
		for _, tr := range translation.Translations {
			for key, value := range tr.KeysAndValues {
				if err := catalogBuilder.SetString(language.Make(tr.Lang), key, value); err != nil {
					return nil, fmt.Errorf("failed to set translation %s %s %s %s", name, tr.Lang, key, value)
				}
				keys = append(keys, key)
			}
		}
	}

	return &Translations{
		catalog: catalogBuilder,
		keys:    keys,
	}, nil
}
