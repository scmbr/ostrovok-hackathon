// Package registry встраивает YAML-описания характеристик в бинарник: рядом с приложением файлов не нужно.
package registry

import "embed"

// FS — файлы *.yaml этого каталога. Формат — docs/registry.md.
//
//go:embed *.yaml
var FS embed.FS
