package catastro

import (
	"encoding/xml"
	"math"
	"strings"
	"testing"
)

const buildingXML = `<?xml version="1.0" encoding="ISO-8859-1"?>
<gml:FeatureCollection xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:bu-ext2d="x" xmlns:bu-core2d="y">
  <gml:featureMember>
    <bu-ext2d:Building gml:id="ES.SDGC.BU.0004701DF3800C">
      <gml:boundedBy><gml:Envelope srsName="urn:ogc:def:crs:EPSG::25831">
        <gml:lowerCorner>429945.651 4580072.933</gml:lowerCorner>
        <gml:upperCorner>429973.141 4580093.773</gml:upperCorner>
      </gml:Envelope></gml:boundedBy>
      <bu-ext2d:geometry><bu-core2d:BuildingGeometry><bu-core2d:geometry>
        <gml:Surface><gml:patches><gml:PolygonPatch>
          <gml:exterior><gml:LinearRing>
            <gml:posList srsDimension="2" count="5"> 0 0 10 0 10 10 0 10 0 0 </gml:posList>
          </gml:LinearRing></gml:exterior>
          <gml:interior><gml:LinearRing>
            <gml:posList srsDimension="2" count="5"> 2 2 4 2 4 4 2 4 2 2 </gml:posList>
          </gml:LinearRing></gml:interior>
        </gml:PolygonPatch></gml:patches></gml:Surface>
      </bu-core2d:geometry></bu-core2d:BuildingGeometry></bu-ext2d:geometry>
      <bu-ext2d:numberOfFloorsAboveGround xsi:nil="true" nilReason="other:unpopulated"></bu-ext2d:numberOfFloorsAboveGround>
    </bu-ext2d:Building>
  </gml:featureMember>
</gml:FeatureCollection>`

const partXML = `<?xml version="1.0" encoding="ISO-8859-1"?>
<gml:FeatureCollection xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:bu-ext2d="x">
  <gml:featureMember><bu-ext2d:BuildingPart gml:id="ES.SDGC.BU.0004701DF3800C_part1">
    <bu-ext2d:numberOfFloorsAboveGround>3</bu-ext2d:numberOfFloorsAboveGround>
  </bu-ext2d:BuildingPart></gml:featureMember>
  <gml:featureMember><bu-ext2d:BuildingPart gml:id="ES.SDGC.BU.0004701DF3800C_part2">
    <bu-ext2d:numberOfFloorsAboveGround>5</bu-ext2d:numberOfFloorsAboveGround>
  </bu-ext2d:BuildingPart></gml:featureMember>
  <gml:featureMember><bu-ext2d:BuildingPart gml:id="ES.SDGC.BU.9999999AA1111B_part1">
    <bu-ext2d:numberOfFloorsAboveGround xsi:nil="true"></bu-ext2d:numberOfFloorsAboveGround>
  </bu-ext2d:BuildingPart></gml:featureMember>
</gml:FeatureCollection>`

func TestScanBuilding(t *testing.T) {
	var got []feature
	if err := scan(strings.NewReader(buildingXML), "Building", true, nil, func(f feature) error {
		got = append(got, f)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("зданий: %d", len(got))
	}
	f := got[0]
	if f.ref != "0004701DF3800C" {
		t.Errorf("ref = %q", f.ref)
	}
	if f.floors != 0 {
		t.Errorf("floors = %d, ожидалось 0 (xsi:nil)", f.floors)
	}
	if f.env != [4]float64{429945.651, 4580072.933, 429973.141, 4580093.773} {
		t.Errorf("env = %v", f.env)
	}
	if len(f.geom) != 1 || len(f.geom[0]) != 2 || len(f.geom[0][0]) != 5 {
		t.Fatalf("геометрия: %v", f.geom)
	}
}

func TestScanPartsMaxFloors(t *testing.T) {
	floors := map[string]int{}
	if err := scan(strings.NewReader(partXML), "BuildingPart", false, nil, func(f feature) error {
		floors[f.ref] = max(floors[f.ref], f.floors)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if floors["0004701DF3800C"] != 5 {
		t.Errorf("максимум по частям = %d, want 5", floors["0004701DF3800C"])
	}
	if floors["9999999AA1111B"] != 0 {
		t.Errorf("пустая этажность = %d, want 0", floors["9999999AA1111B"])
	}
}

func TestLatin1(t *testing.T) {
	src := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><a><b gml:id=\"x\">Ma\xf1ana</b></a>"
	dec := newDecoder(strings.NewReader(src))
	var text string
	for {
		tok, err := dec.RawToken()
		if err != nil {
			break
		}
		if cd, ok := tok.(xml.CharData); ok {
			text = string(cd)
		}
	}
	if text != "Mañana" {
		t.Errorf("text = %q", text)
	}
}

func TestToUTM31N(t *testing.T) {
	e, _ := toUTM31N(3, 41.3874)
	if math.Abs(e-500000) > 1e-6 {
		t.Errorf("на центральном меридиане E = %v, want 500000", e)
	}
	// Площадь Каталонии: около 430,7 км в.д. и 4581,3 км с.ш.
	e, n := toUTM31N(2.17, 41.387)
	if math.Abs(e-430700) > 1000 || math.Abs(n-4581300) > 1500 {
		t.Errorf("(%v, %v) далеко от ожидаемых (430700, 4581300)", e, n)
	}
}

const partWithGeometryXML = `<?xml version="1.0" encoding="ISO-8859-1"?>
<gml:FeatureCollection xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:bu-ext2d="x" xmlns:bu-core2d="y">
  <gml:featureMember><bu-ext2d:BuildingPart gml:id="ES.SDGC.BU.0004701DF3800C_part1">
    <bu-ext2d:geometry><bu-core2d:BuildingGeometry><bu-core2d:geometry><gml:Surface><gml:patches><gml:PolygonPatch>
      <gml:exterior><gml:LinearRing><gml:posList srsDimension="2" count="5"> 0 0 10 0 10 10 0 10 0 0 </gml:posList></gml:LinearRing></gml:exterior>
    </gml:PolygonPatch></gml:patches></gml:Surface></bu-core2d:geometry></bu-core2d:BuildingGeometry></bu-ext2d:geometry>
    <bu-ext2d:numberOfFloorsAboveGround>6</bu-ext2d:numberOfFloorsAboveGround>
  </bu-ext2d:BuildingPart></gml:featureMember>
  <gml:featureMember><bu-ext2d:BuildingPart gml:id="ES.SDGC.BU.9999999AA1111B_part1">
    <bu-ext2d:geometry><bu-core2d:BuildingGeometry><bu-core2d:geometry><gml:Surface><gml:patches><gml:PolygonPatch>
      <gml:exterior><gml:LinearRing><gml:posList srsDimension="2" count="5"> 0 0 5 0 5 5 0 5 0 0 </gml:posList></gml:LinearRing></gml:exterior>
    </gml:PolygonPatch></gml:patches></gml:Surface></bu-core2d:geometry></bu-core2d:BuildingGeometry></bu-ext2d:geometry>
    <bu-ext2d:numberOfFloorsAboveGround>2</bu-ext2d:numberOfFloorsAboveGround>
  </bu-ext2d:BuildingPart></gml:featureMember>
</gml:FeatureCollection>`

func TestScanPartGeometryAndKeep(t *testing.T) {
	var got []feature
	keep := func(ref string) bool { return ref == "0004701DF3800C" }
	if err := scan(strings.NewReader(partWithGeometryXML), "BuildingPart", true, keep, func(f feature) error {
		got = append(got, f)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("частей %d, want 1: вторая отброшена keep", len(got))
	}
	if got[0].floors != 6 || len(got[0].geom) != 1 || len(got[0].geom[0][0]) != 5 {
		t.Errorf("часть: этажей %d, геометрия %v", got[0].floors, got[0].geom)
	}
}
