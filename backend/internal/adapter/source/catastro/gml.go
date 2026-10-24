package catastro

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/paulmach/orb"
)

// latin1Reader перекодирует ISO-8859-1 в UTF-8 на лету; кадастровые GML объявлены в этой кодировке.
type latin1Reader struct {
	r   io.Reader
	buf []byte
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	if len(p) < 2 {
		return 0, io.ErrShortBuffer
	}
	if l.buf == nil {
		l.buf = make([]byte, 32*1024)
	}
	n, err := l.r.Read(l.buf[:min(len(l.buf), len(p)/2)])
	out := 0
	for _, c := range l.buf[:n] {
		if c < 0x80 {
			p[out] = c
			out++
		} else {
			p[out], p[out+1] = 0xC0|c>>6, 0x80|c&0x3F
			out += 2
		}
	}
	return out, err
}

func newDecoder(r io.Reader) *xml.Decoder {
	d := xml.NewDecoder(r)
	d.CharsetReader = func(label string, in io.Reader) (io.Reader, error) {
		switch strings.ToLower(label) {
		case "iso-8859-1", "latin1", "latin-1":
			return &latin1Reader{r: in}, nil
		case "utf-8", "utf8":
			return in, nil
		}
		return nil, fmt.Errorf("неподдерживаемая кодировка %q", label)
	}
	return d
}

// feature — разобранный Building или BuildingPart.
type feature struct {
	ref    string // кадастровая ссылка без суффикса части
	floors int    // 0, если поле пустое (xsi:nil) или отсутствует
	geom   orb.MultiPolygon
	env    [4]float64 // minE, minN, maxE, maxN из gml:Envelope; нули, если нет
}

// scan читает GML потоком и вызывает fn для каждого элемента с локальным именем element
// (Building или BuildingPart). withGeom=false не накапливает контуры.
func scan(r io.Reader, element string, withGeom bool, fn func(feature) error) error {
	dec := newDecoder(r)

	var (
		cur      *feature
		depth    int    // глубина вложенности внутри cur
		ringKind string // exterior | interior
		polygon  orb.Polygon
		text     strings.Builder
	)
	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			if cur == nil {
				if name == element {
					cur = &feature{ref: refFromID(attr(t, "id"))}
					depth = 0
					polygon = nil
				}
				continue
			}
			depth++
			text.Reset()
			switch name {
			case "exterior", "interior":
				ringKind = name
			case "PolygonPatch", "Polygon":
				polygon = nil
			}
		case xml.CharData:
			if cur != nil {
				text.Write(t)
			}
		case xml.EndElement:
			if cur == nil {
				continue
			}
			name := t.Name.Local
			if depth == 0 {
				err := fn(*cur)
				cur = nil
				if err != nil {
					return err
				}
				continue
			}
			depth--
			s := strings.TrimSpace(text.String())
			switch name {
			case "numberOfFloorsAboveGround":
				if n, err := strconv.Atoi(s); err == nil && n > 0 {
					cur.floors = n
				}
			case "lowerCorner":
				fmt.Sscanf(s, "%f %f", &cur.env[0], &cur.env[1])
			case "upperCorner":
				fmt.Sscanf(s, "%f %f", &cur.env[2], &cur.env[3])
			case "posList":
				if !withGeom {
					break
				}
				ring, err := parsePosList(s)
				if err != nil {
					return fmt.Errorf("%s: %w", cur.ref, err)
				}
				if ringKind == "interior" {
					polygon = append(polygon, ring)
				} else {
					polygon = append(orb.Polygon{ring}, polygon...)
				}
			case "PolygonPatch", "Polygon":
				if withGeom && len(polygon) > 0 {
					cur.geom = append(cur.geom, polygon)
				}
				polygon = nil
			}
			text.Reset()
		}
	}
}

func attr(t xml.StartElement, local string) string {
	for _, a := range t.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// refFromID выделяет кадастровую ссылку из gml:id вида
// "ES.SDGC.BU.0004701DF3800C" или "ES.SDGC.BU.0004701DF3800C_part1".
func refFromID(id string) string {
	id = id[strings.LastIndex(id, ".")+1:]
	if i := strings.Index(id, "_"); i >= 0 {
		id = id[:i]
	}
	return id
}

// parsePosList разбирает "x1 y1 x2 y2 ..." в кольцо; третья координата не ожидается (srsDimension=2).
func parsePosList(s string) (orb.Ring, error) {
	fields := strings.Fields(s)
	if len(fields)%2 != 0 || len(fields) < 8 {
		return nil, fmt.Errorf("posList: %d координат", len(fields))
	}
	ring := make(orb.Ring, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		x, err1 := strconv.ParseFloat(fields[i], 64)
		y, err2 := strconv.ParseFloat(fields[i+1], 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("posList: не число в позиции %d", i/2)
		}
		ring = append(ring, orb.Point{x, y})
	}
	return ring, nil
}
