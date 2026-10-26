package overpass

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

type latLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type element struct {
	Type     string            `json:"type"`
	ID       int64             `json:"id"`
	Lat      float64           `json:"lat"`
	Lon      float64           `json:"lon"`
	Center   *latLon           `json:"center"`
	Tags     map[string]string `json:"tags"`
	Geometry []latLon          `json:"geometry"`
	Nodes    []int64           `json:"nodes"`
	Members  []member          `json:"members"`
}

// member — участник relation; у внешних контуров пляжей приходит геометрия.
type member struct {
	Type     string   `json:"type"`
	Role     string   `json:"role"`
	Geometry []latLon `json:"geometry"`
}

type response struct {
	Elements []element `json:"elements"`
}

func bboxQL(b domain.BBox) string {
	return fmt.Sprintf("(%g,%g,%g,%g)", b.South, b.West, b.North, b.East)
}

func decode(body []byte) ([]element, error) {
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("разбор ответа Overpass: %w", err)
	}
	return r.Elements, nil
}

// Hotels возвращает отели (tourism=hotel) внутри заданных районов.
// Отели без названия и отели-relation пропускаются.
func (c *Client) Hotels(ctx context.Context, region domain.Region) ([]domain.Hotel, error) {
	var sb strings.Builder
	sb.WriteString("[out:json][timeout:90];(")
	for _, d := range region.Districts {
		fmt.Fprintf(&sb, `nwr["tourism"="hotel"]%s;`, bboxQL(d.BBox))
	}
	sb.WriteString(");out center tags;")
	body, err := c.query(ctx, "hotels", sb.String())
	if err != nil {
		return nil, err
	}
	els, err := decode(body)
	if err != nil {
		return nil, err
	}
	return parseHotels(els, region), nil
}

func parseHotels(els []element, region domain.Region) []domain.Hotel {
	var hotels []domain.Hotel
	seen := map[int64]bool{}
	skipped := 0
	for _, e := range els {
		h, ok := hotelFrom(e, region)
		if !ok || seen[h.StoreID()] {
			skipped++
			continue
		}
		seen[h.StoreID()] = true
		hotels = append(hotels, h)
	}
	if skipped > 0 {
		log.Printf("отелей пропущено (без названия, relation, вне районов, дубли): %d", skipped)
	}
	return hotels
}

func hotelFrom(e element, region domain.Region) (domain.Hotel, bool) {
	name := e.Tags["name"]
	if name == "" || (e.Type != "node" && e.Type != "way") {
		return domain.Hotel{}, false
	}
	var p orb.Point
	switch {
	case e.Type == "node":
		p = orb.Point{e.Lon, e.Lat}
	case e.Center != nil:
		p = orb.Point{e.Center.Lon, e.Center.Lat}
	default:
		return domain.Hotel{}, false
	}
	district := region.DistrictOf(p)
	if district == "" {
		return domain.Hotel{}, false
	}
	return domain.Hotel{OSMType: e.Type, OSMID: e.ID, Name: name, Point: p, District: district}, true
}

// Buildings возвращает здания-way внутри bbox. Здания-relation (мультиполигоны) пока пропускаются.
func (c *Client) Buildings(ctx context.Context, bbox domain.BBox) ([]domain.Building, error) {
	ql := fmt.Sprintf(`[out:json][timeout:180];way["building"]%s;out geom tags;`, bboxQL(bbox))
	body, err := c.query(ctx, "buildings", ql)
	if err != nil {
		return nil, err
	}
	els, err := decode(body)
	if err != nil {
		return nil, err
	}
	return parseBuildings(els), nil
}

func parseBuildings(els []element) []domain.Building {
	var out []domain.Building
	skipped := 0
	for _, e := range els {
		ring := ringFrom(e)
		if ring == nil {
			skipped++
			continue
		}
		levels, heightM, source := domain.ResolveHeight(parseLevels(e.Tags["building:levels"]), parseHeight(e.Tags["height"]))
		out = append(out, domain.Building{
			OSMID:        e.ID,
			Geom:         orb.MultiPolygon{orb.Polygon{ring}},
			Levels:       levels,
			HeightM:      heightM,
			HeightSource: source,
		})
	}
	if skipped > 0 {
		log.Printf("зданий пропущено (незамкнутый контур): %d", skipped)
	}
	return out
}

// ringFrom возвращает замкнутое кольцо из геометрии way или nil.
func ringFrom(e element) orb.Ring {
	if len(e.Geometry) < 4 {
		return nil
	}
	ring := make(orb.Ring, len(e.Geometry))
	for i, g := range e.Geometry {
		ring[i] = orb.Point{g.Lon, g.Lat}
	}
	if ring[0] != ring[len(ring)-1] {
		return nil
	}
	return ring
}

// Coastline возвращает линии берега (natural=coastline) внутри bbox.
func (c *Client) Coastline(ctx context.Context, bbox domain.BBox) ([]orb.LineString, error) {
	ql := fmt.Sprintf(`[out:json][timeout:90];way["natural"="coastline"]%s;out geom;`, bboxQL(bbox))
	body, err := c.query(ctx, "coastline", ql)
	if err != nil {
		return nil, err
	}
	els, err := decode(body)
	if err != nil {
		return nil, err
	}
	var lines []orb.LineString
	for _, e := range els {
		if len(e.Geometry) < 2 {
			continue
		}
		ls := make(orb.LineString, len(e.Geometry))
		for i, g := range e.Geometry {
			ls[i] = orb.Point{g.Lon, g.Lat}
		}
		lines = append(lines, ls)
	}
	return lines, nil
}

// roadHighways — типы дорог, вдоль которых бывают фасады отелей; тропинки, лестницы и служебные проезды не берём.
const roadHighways = "motorway|trunk|primary|secondary|tertiary|unclassified|residential|living_street|pedestrian|" +
	"motorway_link|trunk_link|primary_link|secondary_link|tertiary_link"

// Roads возвращает дороги (highway из roadHighways) внутри bbox.
func (c *Client) Roads(ctx context.Context, bbox domain.BBox) ([]domain.Road, error) {
	ql := fmt.Sprintf(`[out:json][timeout:180];way["highway"~"^(%s)$"]%s;out geom tags;`, roadHighways, bboxQL(bbox))
	body, err := c.query(ctx, "roads", ql)
	if err != nil {
		return nil, err
	}
	els, err := decode(body)
	if err != nil {
		return nil, err
	}
	var roads []domain.Road
	for _, e := range els {
		if len(e.Geometry) < 2 {
			continue
		}
		ls := make(orb.LineString, len(e.Geometry))
		for i, g := range e.Geometry {
			ls[i] = orb.Point{g.Lon, g.Lat}
		}
		roads = append(roads, domain.Road{OSMID: e.ID, Highway: e.Tags["highway"], Name: e.Tags["name"], Geom: ls})
	}
	return roads, nil
}

// poiFilter — какие объекты выгружаем: ночная жизнь для шума и круглосуточные сервисы для night_services.
const poiFilter = `nwr["amenity"~"^(bar|pub|nightclub|biergarten|pharmacy|cafe|restaurant|fast_food)$"]%[1]s;nwr["shop"~"^(supermarket|convenience)$"]%[1]s;`

// poiTags — теги, которые сохраняем в pois.tags.
var poiTags = []string{"amenity", "shop", "opening_hours", "name"}

// Pois возвращает бары, клубы, аптеки, кафе и магазины внутри bbox (центр для контуров и relation).
func (c *Client) Pois(ctx context.Context, bbox domain.BBox) ([]domain.Poi, error) {
	ql := fmt.Sprintf("[out:json][timeout:180];("+poiFilter+");out center tags;", bboxQL(bbox))
	body, err := c.query(ctx, "pois", ql)
	if err != nil {
		return nil, err
	}
	els, err := decode(body)
	if err != nil {
		return nil, err
	}
	return parsePois(els), nil
}

func parsePois(els []element) []domain.Poi {
	var out []domain.Poi
	for _, e := range els {
		var p orb.Point
		switch {
		case e.Type == "node":
			p = orb.Point{e.Lon, e.Lat}
		case e.Center != nil:
			p = orb.Point{e.Center.Lon, e.Center.Lat}
		default:
			continue
		}
		kind := e.Tags["amenity"]
		if kind == "" {
			kind = e.Tags["shop"]
		}
		if kind == "" {
			continue
		}
		tags := map[string]string{}
		for _, k := range poiTags {
			if v := e.Tags[k]; v != "" {
				tags[k] = v
			}
		}
		out = append(out, domain.Poi{OSMType: e.Type, OSMID: e.ID, Kind: kind, Name: e.Tags["name"], Point: p, Tags: tags})
	}
	return out
}

// walkHighways — пути, по которым можно ходить пешком; автомагистрали и пути с foot=no, access=private|no исключены.
const walkHighways = "footway|pedestrian|path|steps|living_street|residential|service|unclassified|track|cycleway|" +
	"tertiary|tertiary_link|secondary|secondary_link|primary|primary_link|trunk|trunk_link"

// WalkWays возвращает пешеходные пути внутри bbox с id узлов и координатами.
func (c *Client) WalkWays(ctx context.Context, bbox domain.BBox) ([]domain.WalkWay, error) {
	ql := fmt.Sprintf(`[out:json][timeout:300];way["highway"~"^(%s)$"]["foot"!~"^no$"]["access"!~"^(private|no)$"]%s;out geom;`,
		walkHighways, bboxQL(bbox))
	body, err := c.query(ctx, "walk", ql)
	if err != nil {
		return nil, err
	}
	els, err := decode(body)
	if err != nil {
		return nil, err
	}
	return parseWalkWays(els), nil
}

func parseWalkWays(els []element) []domain.WalkWay {
	var out []domain.WalkWay
	for _, e := range els {
		if len(e.Nodes) < 2 || len(e.Nodes) != len(e.Geometry) {
			continue
		}
		pts := make([]orb.Point, len(e.Geometry))
		for i, g := range e.Geometry {
			pts[i] = orb.Point{g.Lon, g.Lat}
		}
		out = append(out, domain.WalkWay{OSMID: e.ID, Nodes: e.Nodes, Points: pts})
	}
	return out
}

// Destinations возвращает цели пеших маршрутов внутри bbox: входы и станции метро, точки вдоль границ пляжей.
// Городские пляжи Барселоны в OSM областями почти не размечены (есть только Соморрострo), поэтому кроме
// natural=beach|sand берём стенды правил пляжей «Platja …» как точки доступа к пляжу; лишний песок (вольеры,
// площадки) отсеивается при сохранении: пляж — только у береговой линии.
func (c *Client) Destinations(ctx context.Context, bbox domain.BBox) ([]domain.Destination, error) {
	b := bboxQL(bbox)
	ql := fmt.Sprintf(`[out:json][timeout:180];(`+
		`node["railway"="subway_entrance"]%[1]s;`+
		`node["railway"="station"]["station"="subway"]%[1]s;`+
		`node["tourism"="information"]["information"="board"]["name"~"^Platja"]%[1]s;`+
		`way["natural"~"^(beach|sand)$"]%[1]s;relation["natural"~"^(beach|sand)$"]%[1]s;);out geom tags;`, b)
	body, err := c.query(ctx, "destinations", ql)
	if err != nil {
		return nil, err
	}
	els, err := decode(body)
	if err != nil {
		return nil, err
	}
	return parseDestinations(els), nil
}

func parseDestinations(els []element) []domain.Destination {
	var out []domain.Destination
	for _, e := range els {
		name := e.Tags["name"]
		switch {
		case e.Type == "node" && e.Tags["tourism"] == "information" && strings.HasPrefix(name, "Platja"):
			// Стенды правил на пляжах Барселоны (AMB): городские пляжи в OSM областями не размечены.
			out = append(out, domain.Destination{OSMID: e.ID, Kind: domain.DestBeach, Name: name, Point: orb.Point{e.Lon, e.Lat}})
		case e.Type == "node" && e.Tags["railway"] != "":
			out = append(out, domain.Destination{OSMID: e.ID, Kind: domain.DestMetro, Name: name, Point: orb.Point{e.Lon, e.Lat}})
		case e.Tags["natural"] == "beach" || e.Tags["natural"] == "sand":
			lines := [][]latLon{e.Geometry}
			for _, m := range e.Members {
				lines = append(lines, m.Geometry)
			}
			for _, g := range lines {
				if len(g) < 2 {
					continue
				}
				ls := make(orb.LineString, len(g))
				for i, p := range g {
					ls[i] = orb.Point{p.Lon, p.Lat}
				}
				for _, p := range domain.SampleBoundary(ls, domain.WalkBeachSampleM) {
					out = append(out, domain.Destination{OSMID: e.ID, Kind: domain.DestBeach, Name: name, Point: p})
				}
			}
		}
	}
	return out
}

// parseLevels разбирает building:levels ("5", "5.0"); нечисловое и неположительное — nil.
func parseLevels(s string) *int {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 1 || f > 200 {
		return nil
	}
	n := int(f)
	return &n
}

// parseHeight разбирает height ("21", "21.5", "21 m", "21m"); футы и прочее — nil.
func parseHeight(s string) *float64 {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSpace(strings.TrimSuffix(s, "m"))
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	if err != nil || f <= 0 || f > 500 {
		return nil
	}
	return &f
}
