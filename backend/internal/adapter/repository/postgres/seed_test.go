package postgres

import (
	"reflect"
	"strings"
	"testing"
)

const sampleDump = `--
-- PostgreSQL database dump
--

SET client_encoding = 'UTF8';
SELECT pg_catalog.set_config('search_path', '', false);

--
-- Data for Name: hotels
--

COPY public.hotels (id, name) FROM stdin;
1	Hotel A
2	Hotel \N
\.


COPY public.sea (id) FROM stdin;
\.

SELECT pg_catalog.setval('public.hotels_id_seq', 2, true);
`

func TestParseDump(t *testing.T) {
	var got []dumpItem
	err := parseDump(strings.NewReader(sampleDump), func(it dumpItem) error {
		got = append(got, it)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []dumpItem{
		{SQL: "SET client_encoding = 'UTF8';"},
		{SQL: "SELECT pg_catalog.set_config('search_path', '', false);"},
		{SQL: "COPY public.hotels (id, name) FROM stdin", IsCopy: true, Data: "1\tHotel A\n2\tHotel \\N\n"},
		{SQL: "COPY public.sea (id) FROM stdin", IsCopy: true, Data: ""},
		{SQL: "SELECT pg_catalog.setval('public.hotels_id_seq', 2, true);"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestParseDumpTruncated(t *testing.T) {
	err := parseDump(strings.NewReader("COPY public.hotels (id) FROM stdin;\n1\n"), func(dumpItem) error { return nil })
	if err == nil {
		t.Error("ожидалась ошибка для оборванного COPY")
	}
}

func TestDropRetiredColumns(t *testing.T) {
	old := dumpItem{
		SQL: "COPY public.room_categories (id, hotel_id, name, facade_ids) FROM stdin", IsCopy: true,
		Data: "1\t5\tStandard\t{}\n2\t5\tSuite\t{}\n",
	}
	got := dropRetiredColumns(old)
	if got.SQL != "COPY public.room_categories (id, hotel_id, name) FROM stdin" || got.Data != "1\t5\tStandard\n2\t5\tSuite\n" {
		t.Errorf("COPY после вырезания колонки: %q / %q", got.SQL, got.Data)
	}
	same := dumpItem{SQL: "COPY public.hotels (id, name) FROM stdin", IsCopy: true, Data: "1\tx\n"}
	if dropRetiredColumns(same) != same {
		t.Error("другие таблицы не должны меняться")
	}
	cur := dumpItem{SQL: "COPY public.room_categories (id, hotel_id, name, rooms_count) FROM stdin", IsCopy: true, Data: "1\t5\tStandard\t30\n"}
	if dropRetiredColumns(cur) != cur {
		t.Error("сид новой схемы не должен меняться")
	}
}
