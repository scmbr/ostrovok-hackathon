// Package catastro — источник зданий и этажности: кадастр Испании (INSPIRE Buildings, ATOM-выгрузка).
package catastro

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Source читает выгрузку муниципалитета из каталога: распакованные *.building.gml и
// *.buildingpart.gml либо zip с ними. Скачивание — вручную, см. docs/data.md.
type Source struct {
	Dir string
}

func NewSource(rawDataDir string) *Source {
	return &Source{Dir: filepath.Join(rawDataDir, "catastro")}
}

// Buildings возвращает здания, чей габарит пересекает bbox, с этажностью = максимум по частям.
// Если этажность частей различается, части возвращаются в Parts (башня на подиуме).
// Здания без этажности пропускаются: у них остаётся высота OSM. Контуры — в EPSG:25831.
func (s *Source) Buildings(ctx context.Context, bbox domain.BBox) ([]domain.Building, error) {
	loE, loN := toUTM31N(bbox.West, bbox.South)
	hiE, hiN := toUTM31N(bbox.East, bbox.North)

	// Проход 1: контуры зданий в рамке.
	rc, err := s.open("building.gml")
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	byRef := map[string]orb.MultiPolygon{}
	var order []string
	var seen int
	err = scan(rc, "Building", true, nil, func(f feature) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		seen++
		if f.env[2] < loE || f.env[0] > hiE || f.env[3] < loN || f.env[1] > hiN || len(f.geom) == 0 {
			return nil
		}
		byRef[f.ref] = f.geom
		order = append(order, f.ref)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("здания: %w", err)
	}
	log.Printf("кадастр: зданий в файле %d, в рамке %d", seen, len(order))

	// Проход 2: части этих зданий — этажность и контуры.
	parts, err := s.parts(ctx, func(ref string) bool { _, ok := byRef[ref]; return ok })
	if err != nil {
		return nil, fmt.Errorf("части зданий: %w", err)
	}

	var out []domain.Building
	var withParts, partRows int
	for _, ref := range order {
		ps := parts[ref]
		maxFloors := 0
		distinct := map[int]bool{}
		for _, p := range ps {
			maxFloors = max(maxFloors, p.floors)
			if p.floors > 0 {
				distinct[p.floors] = true
			}
		}
		if maxFloors == 0 {
			continue
		}
		levels := maxFloors
		b := domain.Building{
			CatastroRef:  ref,
			Geom:         byRef[ref],
			Levels:       &levels,
			HeightM:      float64(maxFloors) * domain.MetersPerLevel,
			HeightSource: domain.HeightCatastro,
		}
		if len(distinct) > 1 {
			for _, p := range ps {
				if p.floors > 0 && len(p.geom) > 0 {
					b.Parts = append(b.Parts, domain.BuildingPart{
						Geom: p.geom, Floors: p.floors, HeightM: float64(p.floors) * domain.MetersPerLevel,
					})
				}
			}
			if len(b.Parts) > 0 {
				withParts++
				partRows += len(b.Parts)
			}
		}
		out = append(out, b)
	}
	log.Printf("кадастр: с этажностью %d, из них с частями разной этажности %d (частей %d)", len(out), withParts, partRows)
	return out, nil
}

type partInfo struct {
	floors int
	geom   orb.MultiPolygon
}

// parts собирает части зданий, прошедших keep, по кадастровой ссылке.
func (s *Source) parts(ctx context.Context, keep func(ref string) bool) (map[string][]partInfo, error) {
	rc, err := s.open("buildingpart.gml")
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	out := map[string][]partInfo{}
	err = scan(rc, "BuildingPart", true, keep, func(f feature) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		out[f.ref] = append(out[f.ref], partInfo{floors: f.floors, geom: f.geom})
		return nil
	})
	return out, err
}

// open ищет файл с заданным окончанием имени в каталоге (рекурсивно), затем в zip-архивах.
func (s *Source) open(suffix string) (io.ReadCloser, error) {
	var found string
	_ = filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && found == "" && strings.HasSuffix(strings.ToLower(d.Name()), suffix) {
			found = p
		}
		return nil
	})
	if found != "" {
		return os.Open(found)
	}
	zips, _ := filepath.Glob(filepath.Join(s.Dir, "*.zip"))
	for _, z := range zips {
		zr, err := zip.OpenReader(z)
		if err != nil {
			continue
		}
		for _, f := range zr.File {
			if strings.HasSuffix(strings.ToLower(f.Name), suffix) {
				rc, err := f.Open()
				if err != nil {
					zr.Close()
					return nil, err
				}
				return &zipFile{ReadCloser: rc, zr: zr}, nil
			}
		}
		zr.Close()
	}
	return nil, fmt.Errorf("в %s нет файла *%s (скачайте выгрузку, см. docs/data.md)", s.Dir, suffix)
}

type zipFile struct {
	io.ReadCloser
	zr *zip.ReadCloser
}

func (z *zipFile) Close() error {
	err := z.ReadCloser.Close()
	if e := z.zr.Close(); err == nil {
		err = e
	}
	return err
}
