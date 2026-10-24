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
// Здания без этажности пропускаются: у них остаётся высота OSM. Контуры — в EPSG:25831.
func (s *Source) Buildings(ctx context.Context, bbox domain.BBox) ([]domain.Building, error) {
	floors, err := s.maxFloors(ctx)
	if err != nil {
		return nil, fmt.Errorf("части зданий: %w", err)
	}
	log.Printf("кадастр: этажность у %d зданий", len(floors))

	loE, loN := toUTM31N(bbox.West, bbox.South)
	hiE, hiN := toUTM31N(bbox.East, bbox.North)

	rc, err := s.open("building.gml")
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var out []domain.Building
	var seen, inBox int
	err = scan(rc, "Building", true, func(f feature) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		seen++
		if f.env[2] < loE || f.env[0] > hiE || f.env[3] < loN || f.env[1] > hiN {
			return nil
		}
		inBox++
		n := floors[f.ref]
		if n == 0 || len(f.geom) == 0 {
			return nil
		}
		levels := n
		out = append(out, domain.Building{
			CatastroRef:  f.ref,
			Geom:         f.geom,
			Levels:       &levels,
			HeightM:      float64(n) * domain.MetersPerLevel,
			HeightSource: domain.HeightCatastro,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("здания: %w", err)
	}
	log.Printf("кадастр: зданий в файле %d, в рамке %d, с этажностью %d", seen, inBox, len(out))
	return out, nil
}

func (s *Source) maxFloors(ctx context.Context) (map[string]int, error) {
	rc, err := s.open("buildingpart.gml")
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	floors := map[string]int{}
	err = scan(rc, "BuildingPart", false, func(f feature) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if f.floors > floors[f.ref] {
			floors[f.ref] = f.floors
		}
		return nil
	})
	return floors, err
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
