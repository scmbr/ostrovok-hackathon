// Package bcnnoise — источник шума по фасадам: стратегическая карта шума Барселоны (Open Data BCN, GeoPackage).
package bcnnoise

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/wkb"
	_ "modernc.org/sqlite" // чистый Go-драйвер SQLite без CGO

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// FacadesFile — файл карты по фасадам в data/raw/noise (см. docs/data.md).
const FacadesFile = "2017_Facanes_Mapa_Estrategic_Soroll_BCN.gpkg"

var identRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Source читает GeoPackage с фасадным шумом.
type Source struct {
	Path string
}

func NewSource(rawDataDir string) *Source {
	return &Source{Path: filepath.Join(rawDataDir, "noise", FacadesFile)}
}

// FacadeSegments возвращает сегменты фасадов зданий с переданными кадастровыми ссылками.
// Многосоставные линии разбиваются на отдельные сегменты, контуры — в EPSG:25831.
func (s *Source) FacadeSegments(ctx context.Context, refs map[string]bool) ([]domain.NoiseFacadeSegment, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(s.Path)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var table, geomCol string
	if err := db.QueryRowContext(ctx, `SELECT table_name, column_name FROM gpkg_geometry_columns LIMIT 1`).Scan(&table, &geomCol); err != nil {
		return nil, fmt.Errorf("слой геометрии GeoPackage: %w", err)
	}
	if !identRe.MatchString(table) || !identRe.MatchString(geomCol) {
		return nil, fmt.Errorf("недопустимые имена слоя %q / %q", table, geomCol)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT "%s", ref_cadastral, Total_Ln, Total_Lden, Oci_Ln FROM "%s"`, geomCol, table))
	if err != nil {
		return nil, fmt.Errorf("чтение слоя %s: %w", table, err)
	}
	defer rows.Close()

	var out []domain.NoiseFacadeSegment
	seen := 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var blob []byte
		var ref sql.NullString
		var ln, lden, oci sql.NullFloat64
		if err := rows.Scan(&blob, &ref, &ln, &lden, &oci); err != nil {
			return nil, err
		}
		seen++
		if !ref.Valid || !refs[ref.String] || !ln.Valid {
			continue
		}
		g, err := parseGeometry(blob)
		if err != nil {
			return nil, fmt.Errorf("геометрия сегмента %s: %w", ref.String, err)
		}
		for _, line := range lines(g) {
			out = append(out, domain.NoiseFacadeSegment{
				CatastroRef: ref.String, Line: line,
				LnDB: ln.Float64, LdenDB: lden.Float64, OciLnDB: oci.Float64,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	log.Printf("шум BCN: сегментов в файле %d, у зданий отелей %d", seen, len(out))
	return out, nil
}

// parseGeometry разбирает геометрию GeoPackage: заголовок «GP», версия, флаги, srs_id, рамка, затем WKB.
func parseGeometry(blob []byte) (orb.Geometry, error) {
	if len(blob) < 8 || blob[0] != 'G' || blob[1] != 'P' {
		return nil, errors.New("нет заголовка GeoPackage")
	}
	flags := blob[3]
	if flags&0x10 != 0 {
		return nil, errors.New("пустая геометрия")
	}
	var envelope int
	switch (flags >> 1) & 0x07 {
	case 0:
		envelope = 0
	case 1:
		envelope = 32
	case 2, 3:
		envelope = 48
	case 4:
		envelope = 64
	default:
		return nil, errors.New("неизвестный тип рамки")
	}
	if len(blob) < 8+envelope {
		return nil, errors.New("заголовок обрезан")
	}
	return wkb.Unmarshal(blob[8+envelope:])
}

func lines(g orb.Geometry) []orb.LineString {
	switch v := g.(type) {
	case orb.LineString:
		return []orb.LineString{v}
	case orb.MultiLineString:
		return v
	}
	return nil
}
