package postgres

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dumpItem — одна команда дампа pg_dump --data-only: обычная строка SQL
// либо блок COPY с данными (без терминатора `\.`).
type dumpItem struct {
	SQL    string
	IsCopy bool
	Data   string
}

// parseDump разбирает текстовый дамп pg_dump --data-only в последовательность команд.
// Поддерживает то, что такой дамп содержит: однострочные SET/SELECT и блоки COPY ... FROM stdin.
func parseDump(r io.Reader, yield func(dumpItem) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	var cur *dumpItem
	var data strings.Builder
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case cur != nil:
				if trimmed == `\.` {
					cur.Data = data.String()
					data.Reset()
					if err := yield(*cur); err != nil {
						return err
					}
					cur = nil
				} else {
					data.WriteString(line)
				}
			case trimmed == "" || strings.HasPrefix(trimmed, "--"):
			case strings.HasPrefix(trimmed, "COPY ") && strings.HasSuffix(trimmed, "FROM stdin;"):
				cur = &dumpItem{SQL: strings.TrimSuffix(trimmed, ";"), IsCopy: true}
			default:
				if err := yield(dumpItem{SQL: trimmed}); err != nil {
					return err
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if cur != nil {
		return errors.New("дамп оборван внутри блока COPY")
	}
	return nil
}

// LoadSeed заливает демо-данные из seed.sql.gz, если таблица hotels пуста.
// Возвращает false, если сид не нужен (БД не пуста) или файла нет.
func LoadSeed(ctx context.Context, pool *pgxpool.Pool, path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM hotels`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		log.Printf("сид %s не найден, пропускаю", path)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return false, fmt.Errorf("сид %s: %w", path, err)
	}
	defer zr.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден

	rows := 0
	err = parseDump(zr, func(it dumpItem) error {
		it = dropRetiredColumns(it)
		if it.IsCopy {
			tag, err := tx.Conn().PgConn().CopyFrom(ctx, strings.NewReader(it.Data), it.SQL+";")
			if err != nil {
				return fmt.Errorf("%s: %w", it.SQL, err)
			}
			rows += int(tag.RowsAffected())
			return nil
		}
		if _, err := tx.Exec(ctx, it.SQL); err != nil {
			return fmt.Errorf("%s: %w", it.SQL, err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	// pg_dump пишет в сид set_config('search_path', '', false): без сброса соединение остаётся в пуле
	// с пустым search_path, и запросы без схемы на нём падают.
	if _, err := tx.Exec(ctx, `RESET search_path`); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	log.Printf("сид загружен: %s, строк: %d", path, rows)
	return true, nil
}

// retiredColumns — колонки, удалённые миграциями после сборки сида. Сид пересобирается в конце этапа,
// до тех пор старый дамп должен загружаться в новую схему: колонка вырезается из заголовка COPY и строк.
// Удалить запись вместе с пересборкой сида.
var retiredColumns = map[string][]string{
	"public.room_categories": {"facade_ids"}, // миграция 0015: заменена room_placements
}

// dropRetiredColumns вырезает удалённые колонки из блока COPY.
func dropRetiredColumns(it dumpItem) dumpItem {
	if !it.IsCopy {
		return it
	}
	rest, ok := strings.CutPrefix(it.SQL, "COPY ")
	if !ok {
		return it
	}
	table, cols, ok := strings.Cut(rest, " (")
	retired := retiredColumns[table]
	if !ok || len(retired) == 0 {
		return it
	}
	cols, tail, ok := strings.Cut(cols, ")")
	if !ok {
		return it
	}
	names := strings.Split(cols, ", ")
	var keep []int
	for i, n := range names {
		if !slices.Contains(retired, n) {
			keep = append(keep, i)
		}
	}
	if len(keep) == len(names) {
		return it
	}
	pick := func(fields []string) []string {
		out := make([]string, 0, len(keep))
		for _, i := range keep {
			out = append(out, fields[i])
		}
		return out
	}
	header := pick(names)
	var data strings.Builder
	for _, line := range strings.SplitAfter(it.Data, "\n") {
		if line == "" {
			continue
		}
		body := strings.TrimSuffix(line, "\n")
		data.WriteString(strings.Join(pick(strings.Split(body, "\t")), "\t"))
		if strings.HasSuffix(line, "\n") {
			data.WriteString("\n")
		}
	}
	return dumpItem{SQL: "COPY " + table + " (" + strings.Join(header, ", ") + ")" + tail, IsCopy: true, Data: data.String()}
}
