package menu

import (
	"strings"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

func ToMenuEntries(
	items []kdexv1alpha1.MicroFrontEndPageBinding,
) map[string]MenuEntry {
	menuEntries := make(map[string]MenuEntry)

	for _, item := range items {
		if item.Spec.NavigationHints == nil {
			continue
		}

		label := item.Spec.Label
		menuEntry := MenuEntry{
			Icon:   item.Spec.NavigationHints.Icon,
			Path:   item.Spec.Path,
			Weight: item.Spec.NavigationHints.Weight,
		}

		if item.Spec.NavigationHints.Parent != "" {
			currentMenuEntries := menuEntries
			parents := strings.Split(item.Spec.NavigationHints.Parent, "/")
			for _, parent := range parents {
				parent = strings.Trim(parent, " 	")
				if parent == "" {
					continue
				}
				if currentMenuEntry, ok := currentMenuEntries[parent]; ok {
					if currentMenuEntry.Children == nil {
						children := make(map[string]MenuEntry)
						currentMenuEntry.Children = &children
					}
					currentMenuEntries = *currentMenuEntry.Children
				} else {
					children := make(map[string]MenuEntry)
					currentMenuEntries[parent] = MenuEntry{
						Children: &children,
					}
					currentMenuEntries = *currentMenuEntries[parent].Children
				}
			}
			currentMenuEntries[label] = menuEntry
		} else {
			menuEntries[label] = menuEntry
		}
	}

	return menuEntries
}
