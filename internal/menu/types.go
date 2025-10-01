package menu

import "k8s.io/apimachinery/pkg/api/resource"

type MenuEntry struct {
	Children *map[string]MenuEntry
	Icon     string
	Path     string
	Weight   resource.Quantity
}
