package overpass

import (
	"testing"

	"github.com/paulmach/orb"
)

const hotelsJSON = `{"elements":[
 {"type":"node","id":1,"lat":41.380,"lon":2.190,"tags":{"tourism":"hotel","name":"Mar"}},
 {"type":"way","id":2,"center":{"lat":41.383,"lon":2.176},"tags":{"tourism":"hotel","name":"Gotic"}},
 {"type":"node","id":3,"lat":41.380,"lon":2.190,"tags":{"tourism":"hotel"}},
 {"type":"relation","id":4,"center":{"lat":41.395,"lon":2.165},"tags":{"tourism":"hotel","name":"Rel"}},
 {"type":"node","id":1,"lat":41.380,"lon":2.190,"tags":{"tourism":"hotel","name":"Mar"}},
 {"type":"node","id":5,"lat":41.30,"lon":2.10,"tags":{"tourism":"hotel","name":"Far"}}
]}`

func TestParseHotels(t *testing.T) {
	els, err := decode([]byte(hotelsJSON))
	if err != nil {
		t.Fatal(err)
	}
	hotels := parseHotels(els)
	if len(hotels) != 2 {
		t.Fatalf("got %d hotels, want 2: %+v", len(hotels), hotels)
	}
	if hotels[0].District != "barceloneta" || hotels[0].StoreID() != 1 {
		t.Errorf("hotel 0: %+v", hotels[0])
	}
	if hotels[1].District != "gothic" || hotels[1].StoreID() != -2 {
		t.Errorf("hotel 1: %+v", hotels[1])
	}
}

const buildingsJSON = `{"elements":[
 {"type":"way","id":10,"tags":{"building":"yes","building:levels":"6"},"geometry":[
   {"lat":41.38,"lon":2.19},{"lat":41.38,"lon":2.191},{"lat":41.381,"lon":2.191},{"lat":41.38,"lon":2.19}]},
 {"type":"way","id":11,"tags":{"building":"yes","height":"21 m","building:levels":"5"},"geometry":[
   {"lat":41.38,"lon":2.19},{"lat":41.38,"lon":2.191},{"lat":41.381,"lon":2.191},{"lat":41.38,"lon":2.19}]},
 {"type":"way","id":12,"tags":{"building":"yes"},"geometry":[
   {"lat":41.38,"lon":2.19},{"lat":41.38,"lon":2.191},{"lat":41.381,"lon":2.191},{"lat":41.38,"lon":2.19}]},
 {"type":"way","id":13,"tags":{"building":"yes"},"geometry":[
   {"lat":41.38,"lon":2.19},{"lat":41.38,"lon":2.191},{"lat":41.381,"lon":2.191},{"lat":41.382,"lon":2.19}]}
]}`

func TestParseBuildings(t *testing.T) {
	els, err := decode([]byte(buildingsJSON))
	if err != nil {
		t.Fatal(err)
	}
	b := parseBuildings(els)
	if len(b) != 3 {
		t.Fatalf("got %d buildings, want 3 (незамкнутый пропущен)", len(b))
	}
	want := []struct {
		src string
		h   float64
	}{{"osm_levels", 18}, {"osm_height", 21}, {"default", 15}}
	for i, w := range want {
		if b[i].HeightSource != w.src || b[i].HeightM != w.h {
			t.Errorf("building %d: got (%s, %v), want (%s, %v)", b[i].OSMID, b[i].HeightSource, b[i].HeightM, w.src, w.h)
		}
	}
}

func TestParseHeight(t *testing.T) {
	tests := []struct {
		in   string
		want float64 // 0 = nil
	}{
		{"12", 12}, {"12.5", 12.5}, {"12 m", 12}, {"12m", 12}, {"12,5", 12.5},
		{"", 0}, {"tall", 0}, {"-3", 0}, {"40 ft", 0},
	}
	for _, tt := range tests {
		got := parseHeight(tt.in)
		switch {
		case tt.want == 0 && got != nil:
			t.Errorf("parseHeight(%q) = %v, want nil", tt.in, *got)
		case tt.want != 0 && (got == nil || *got != tt.want):
			t.Errorf("parseHeight(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseLevels(t *testing.T) {
	if l := parseLevels("6"); l == nil || *l != 6 {
		t.Errorf("6: %v", l)
	}
	for _, in := range []string{"", "0", "many", "-1"} {
		if l := parseLevels(in); l != nil {
			t.Errorf("%q: want nil, got %d", in, *l)
		}
	}
}

func TestParsePois(t *testing.T) {
	els := []element{
		{Type: "node", ID: 1, Lat: 41.38, Lon: 2.18, Tags: map[string]string{"amenity": "bar", "name": "Bar", "opening_hours": "Mo-Su 18:00-03:00", "cuisine": "x"}},
		{Type: "way", ID: 2, Center: &latLon{Lat: 41.39, Lon: 2.17}, Tags: map[string]string{"shop": "convenience"}},
		{Type: "way", ID: 3, Tags: map[string]string{"amenity": "pharmacy"}}, // нет центра
		{Type: "node", ID: 4, Lat: 41.38, Lon: 2.18, Tags: map[string]string{"name": "без вида"}},
	}
	got := parsePois(els)
	if len(got) != 2 {
		t.Fatalf("POI %d, want 2: без центра и без вида пропускаются", len(got))
	}
	if got[0].Kind != "bar" || got[0].Tags["opening_hours"] == "" || got[0].Tags["cuisine"] != "" || got[0].Point[0] != 2.18 {
		t.Errorf("бар: %+v", got[0])
	}
	if got[1].Kind != "convenience" || got[1].Point != (orb.Point{2.17, 41.39}) {
		t.Errorf("магазин: %+v", got[1])
	}
}

func TestParseWalkWays(t *testing.T) {
	els := []element{
		{Type: "way", ID: 1, Nodes: []int64{10, 11}, Geometry: []latLon{{41.38, 2.17}, {41.38, 2.18}}},
		{Type: "way", ID: 2, Nodes: []int64{10, 11, 12}, Geometry: []latLon{{41.38, 2.17}}}, // узлов и точек поровну нет
		{Type: "way", ID: 3, Nodes: []int64{10}, Geometry: []latLon{{41.38, 2.17}}},         // одна точка
	}
	got := parseWalkWays(els)
	if len(got) != 1 || got[0].OSMID != 1 || got[0].Points[1] != (orb.Point{2.18, 41.38}) {
		t.Errorf("пути: %+v", got)
	}
}

func TestParseDestinations(t *testing.T) {
	els := []element{
		{Type: "node", ID: 1, Lat: 41.38, Lon: 2.17, Tags: map[string]string{"railway": "subway_entrance", "name": "Jaume I"}},
		{Type: "way", ID: 2, Tags: map[string]string{"natural": "beach", "name": "Platja"},
			Geometry: []latLon{{41.37, 2.19}, {41.37, 2.1905}, {41.37, 2.1999}}},
		{Type: "relation", ID: 3, Tags: map[string]string{"natural": "beach"},
			Members: []member{{Type: "way", Role: "outer", Geometry: []latLon{{41.36, 2.2}, {41.36, 2.21}}}}},
		{Type: "node", ID: 4, Lat: 41.0, Lon: 2.0, Tags: map[string]string{"amenity": "bar"}}, // не цель
		{Type: "node", ID: 5, Lat: 41.37, Lon: 2.19, Tags: map[string]string{"tourism": "information", "information": "board", "name": "Platja de Bogatell"}},
		{Type: "node", ID: 6, Lat: 41.38, Lon: 2.18, Tags: map[string]string{"tourism": "information", "name": "Museu"}}, // не пляж
	}
	got := parseDestinations(els)
	var metro, beach int
	for _, d := range got {
		switch d.Kind {
		case "metro":
			metro++
			if d.Name != "Jaume I" || d.Point != (orb.Point{2.17, 41.38}) {
				t.Errorf("метро: %+v", d)
			}
		case "beach":
			beach++
		}
	}
	// Метро одно; пляж-way: начало, точка после 40 м и конец; пляж-relation: начало и конец; стенд пляжа: одна точка.
	if metro != 1 || beach < 5 {
		t.Errorf("метро %d, точек пляжа %d: %+v", metro, beach, got)
	}
}
