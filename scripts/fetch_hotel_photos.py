#!/usr/bin/env python3
"""Одноразовая выгрузка фото демо-отелей из Wikimedia Commons (не часть collect).

Откуда: теги OSM wikidata / wikimedia_commons (Overpass) -> Wikidata P18 (главное фото)
или файлы категории Commons -> Commons API (миниатюра 1200 px, автор, лицензия).
Только свободные лицензии: CC0, CC BY, CC BY-SA, Public domain. Сайты отелей не скрейпим.

Результат: frontend/public/hotels/<hotel_id>/<n>.webp (<= 200 КБ) и
frontend/public/hotels/attribution.json.

Использование (из корня репозитория):
  docker exec <db> psql -U tima -At -F '|' -c \
    "select id, osm_id, name from hotels where demo order by id" > hotels.txt
  python -I scripts/fetch_hotel_photos.py hotels.txt
Нужен Pillow.
"""
import io
import json
import re
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path

from PIL import Image

UA = "tima-mechty-hackathon/1.0 (educational prototype; contact: karpovkarpij@gmail.com)"
OVERPASS = ["https://overpass-api.de/api/interpreter", "https://overpass.kumi.systems/api/interpreter"]
COMMONS = "https://commons.wikimedia.org/w/api.php"
WIKIDATA = "https://www.wikidata.org/w/api.php"
OUT = Path("frontend/public/hotels")
MAX_PHOTOS = 2
MAX_SIDE = 1200
MAX_BYTES = 200 * 1024
FREE = [
    (re.compile(r"^cc0", re.I), "CC0"),
    (re.compile(r"^public domain|^pd\b|^pd-", re.I), "Public domain"),
    (re.compile(r"^cc[ -]by[ -]sa", re.I), None),  # сохраняем исходную запись (версия)
    (re.compile(r"^cc[ -]by\b", re.I), None),
]
NONFREE = re.compile(r"\bnc\b|\bnd\b|fair use|non-?free", re.I)


def get(url, data=None, tries=3):
    for i in range(tries):
        try:
            req = urllib.request.Request(url, data=data, headers={"User-Agent": UA})
            with urllib.request.urlopen(req, timeout=60) as r:
                return r.read()
        except Exception as e:  # noqa: BLE001
            if i == tries - 1:
                raise
            time.sleep(2 * (i + 1))


def api(base, **params):
    params["format"] = "json"
    return json.loads(get(base + "?" + urllib.parse.urlencode(params)))


def osm_tags(hotels):
    nodes = [str(o) for _, o, _ in hotels if o > 0]
    ways = [str(-o) for _, o, _ in hotels if o < 0]
    q = "[out:json][timeout:90];("
    if nodes:
        q += f"node(id:{','.join(nodes)});"
    if ways:
        q += f"way(id:{','.join(ways)});"
    q += ");out tags;"
    for mirror in OVERPASS:
        try:
            raw = get(mirror, data=urllib.parse.urlencode({"data": q}).encode())
            els = json.loads(raw)["elements"]
            break
        except Exception as e:  # noqa: BLE001
            print("overpass", mirror, e, file=sys.stderr)
    else:
        sys.exit("Overpass недоступен")
    out = {}
    for e in els:
        out[(e["type"], e["id"])] = e.get("tags", {})
    res = {}
    for hid, osm, _ in hotels:
        res[hid] = out.get(("node", osm) if osm > 0 else ("way", -osm), {})
    return res


def p18_files(qid):
    d = api(WIKIDATA, action="wbgetclaims", entity=qid, property="P18")
    files = []
    for c in d.get("claims", {}).get("P18", []):
        v = c.get("mainsnak", {}).get("datavalue", {}).get("value")
        if v:
            files.append("File:" + v)
    return files


def category_files(cat, limit=10):
    d = api(COMMONS, action="query", list="categorymembers", cmtitle=cat, cmtype="file", cmlimit=limit)
    return [m["title"] for m in d.get("query", {}).get("categorymembers", [])]


def strip_html(s):
    return re.sub(r"\s+", " ", re.sub(r"<[^>]+>", "", s or "")).strip()


def file_info(title):
    d = api(
        COMMONS, action="query", titles=title, prop="imageinfo",
        iiprop="url|extmetadata|mime|size", iiurlwidth=MAX_SIDE,
    )
    page = next(iter(d["query"]["pages"].values()))
    ii = (page.get("imageinfo") or [None])[0]
    if not ii or ii.get("mime") not in ("image/jpeg", "image/png"):
        return None
    m = ii.get("extmetadata", {})
    lic = strip_html(m.get("LicenseShortName", {}).get("value"))
    if not lic or NONFREE.search(lic):
        return None
    if not any(rx.search(lic) for rx, _ in FREE):
        return None
    author = strip_html(m.get("Artist", {}).get("value")) or "автор не указан"
    return {
        "author": author[:120],
        "license": lic,
        "source": ii["descriptionurl"],
        "thumb": ii.get("thumburl") or ii["url"],
    }


def to_webp(raw):
    im = Image.open(io.BytesIO(raw)).convert("RGB")
    im.thumbnail((MAX_SIDE, MAX_SIDE))
    for q in (82, 74, 66, 58, 50, 42):
        buf = io.BytesIO()
        im.save(buf, "WEBP", quality=q, method=6)
        if buf.tell() <= MAX_BYTES:
            return buf.getvalue()
    return None


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
    hotels = []
    for line in Path(sys.argv[1]).read_text(encoding="utf-8").splitlines():
        p = line.split("|")
        hotels.append((int(p[0]), int(p[1]), p[2]))
    tags = osm_tags(hotels)
    attribution = {}
    with_tag = 0
    for hid, _, name in hotels:
        t = tags[hid]
        titles = []
        qid, com = t.get("wikidata"), t.get("wikimedia_commons")
        if qid or com:
            with_tag += 1
        try:
            if com and com.startswith("File:"):
                titles.append(com)
            if qid:
                titles += p18_files(qid)
            if com and com.startswith("Category:") and len(titles) < MAX_PHOTOS:
                titles += category_files(com)
        except Exception as e:  # noqa: BLE001
            print(hid, name, "ошибка API:", e, file=sys.stderr)
        saved = []
        seen = set()
        for title in titles:
            if len(saved) >= MAX_PHOTOS:
                break
            if title in seen:
                continue
            seen.add(title)
            try:
                info = file_info(title)
                if not info:
                    continue
                data = to_webp(get(info["thumb"]))
                if not data:
                    continue
            except Exception as e:  # noqa: BLE001
                print(hid, name, title, "пропуск:", e, file=sys.stderr)
                continue
            d = OUT / str(hid)
            d.mkdir(parents=True, exist_ok=True)
            n = len(saved) + 1
            (d / f"{n}.webp").write_bytes(data)
            saved.append({k: info[k] for k in ("author", "license", "source")} | {"file": f"{n}.webp", "title": title})
            time.sleep(0.5)
        if saved:
            attribution[str(hid)] = saved
        print(hid, name, "tags:", bool(qid), bool(com), "фото:", len(saved))
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / "attribution.json").write_text(
        json.dumps({"hotels": attribution, "rooms": {}}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(f"итого: отелей с тегами {with_tag}, с фото {len(attribution)} из {len(hotels)}")


if __name__ == "__main__":
    main()
