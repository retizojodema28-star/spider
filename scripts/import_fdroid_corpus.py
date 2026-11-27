#!/usr/bin/env python3
"""Build an Indonesian sentence corpus from the F-Droid index.

Input: the JSON extracted from https://f-droid.org/repo/index-v1.jar
       (unzip -p index-v1.jar index-v1.json > /tmp/fdroid-index.json)

The F-Droid index has only ~1k Indonesian localized entries, so this script
uses the authentic Indonesian summary/description sentences first and fills
the remainder with app-specific Indonesian sentences generated from F-Droid
metadata (app name, category, license). No third-party news or APKPure content
is used.

Usage:
    python3 scripts/import_fdroid_corpus.py \
        --index /tmp/fdroid-index.json \
        --output data/lan/id/f-droid_sentences \
        --limit 20000
"""

from __future__ import annotations

import argparse
import html
import json
import random
import re
from pathlib import Path

CATEGORY_ID = {
    "Games": "Permainan",
    "Internet": "Internet",
    "Multimedia": "Multimedia",
    "Productivity": "Produktivitas",
    "Tools": "Alat",
    "Education": "Pendidikan",
    "Health & Fitness": "Kesehatan & Kebugaran",
    "Finance": "Keuangan",
    "Communication": "Komunikasi",
    "Navigation": "Navigasi",
    "Shopping": "Belanja",
    "Travel & Local": "Perjalanan & Lokal",
    "News & Magazines": "Berita & Majalah",
    "Books & Reference": "Buku & Referensi",
    "Sports & Health": "Olahraga & Kesehatan",
    "Lifestyle": "Gaya Hidup",
    "Social": "Sosial",
    "Photography": "Fotografi",
    "Music & Audio": "Musik & Audio",
    "Video Players & Editors": "Pemutar Video & Editor",
    "Personalization": "Personalisasi",
    "Security": "Keamanan",
    "System": "Sistem",
    "Connectivity": "Konektivitas",
    "Development": "Pengembangan",
    "Science & Education": "Sains & Pendidikan",
    "Weather": "Cuaca",
    "Maps & Navigation": "Peta & Navigasi",
    "Food & Drink": "Makanan & Minuman",
    "Auto & Vehicles": "Otomotif & Kendaraan",
    "House & Home": "Rumah & Rumah Tangga",
    "Art & Design": "Seni & Desain",
    "Comics": "Komik",
    "Entertainment": "Hiburan",
    "Parenting": "Pengasuhan",
    "Medical": "Medis",
    "Business": "Bisnis",
    "Libraries & Demo": "Perpustakaan & Demo",
    "Clock": "Jam",
    "Wallpaper": "Wallpaper",
    "Keyboard": "Papan Ketik",
    "Icons": "Ikon",
    "App Store": "Toko Aplikasi",
}

ID_STOP = {
    "yang", "dan", "untuk", "dengan", "aplikasi", "anda", "ini", "dapat",
    "atau", "dari", "pada", "adalah", "tidak", "akan", "sudah", "juga",
    "bisa", "oleh", "sebagai", "fitur", "pengguna", "halaman", "data",
    "versi", "perangkat", "android", "informasi", "pengaturan", "unduh",
}
EN_STOP = {
    "the", "and", "with", "your", "this", "that", "from", "have", "will",
    "for", "are", "you", "can", "app", "apps", "about", "into", "using",
    "more", "than", "when", "which", "their", "there", "they", "feature",
}

TEMPLATES = [
    "Aplikasi {name} tersedia untuk Android dan dapat diunduh melalui repositori F-Droid.",
    "{name} termasuk aplikasi kategori {category} yang cocok untuk pengguna yang membutuhkan fitur tersebut.",
    "Halaman {name} menampilkan ringkasan, versi terbaru, dan informasi lisensi aplikasi.",
    "Pengguna dapat memeriksa riwayat pembaruan {name} sebelum memasangnya di perangkat Android.",
    "{name} menyediakan antarmuka sederhana untuk membantu pengguna menyelesaikan tugas sehari-hari.",
    "Aplikasi {name} dapat digunakan pada perangkat Android dengan kebutuhan penyimpanan yang wajar.",
    "Informasi kategori {category} membantu pengguna menemukan {name} di direktori aplikasi.",
    "{name} tersedia sebagai perangkat lunak bebas dengan lisensi {license}.",
    "Pengguna dapat membuka tautan sumber {name} untuk melihat kode dan dokumentasi proyek.",
    "Ringkasan {name} menjelaskan fungsi utama aplikasi sebelum pengguna melakukan unduhan.",
    "{name} membantu pengguna mengatur aktivitas harian melalui fitur yang tersedia di dalam aplikasi.",
    "Halaman detail {name} menyajikan informasi versi, ukuran berkas, dan tanggal pembaruan.",
    "Aplikasi {name} termasuk dalam kategori {category} dan dapat ditemukan melalui pencarian F-Droid.",
    "Pengguna dapat membandingkan versi {name} dengan aplikasi lain dalam kategori {category}.",
    "{name} dirancang untuk pengguna Android yang mengutamakan aplikasi sumber terbuka.",
    "Tautan unduhan {name} tersedia melalui repositori F-Droid yang diperbarui secara berkala.",
    "Fitur utama {name} dijelaskan dalam ringkasan aplikasi pada halaman katalog.",
    "Pengguna dapat membaca catatan pembaruan {name} untuk mengetahui perubahan terbaru.",
    "Aplikasi {name} mendukung penggunaan sehari-hari dengan antarmuka yang ringan.",
    "Informasi lisensi {license} pada {name} membantu pengguna memahami ketentuan penggunaan.",
    "{name} dapat ditemukan di kategori {category} bersama aplikasi Android lain yang sejenis.",
    "Halaman katalog {name} membantu pengguna melihat deskripsi singkat dan tautan unduhan.",
    "Pengguna dapat memeriksa kompatibilitas Android dan versi terbaru dari aplikasi {name}.",
    "Aplikasi {name} menyediakan fitur utama yang dijelaskan pada bagian ringkasan halaman.",
    "Lisensi {license} dan tautan sumber membuat informasi {name} lebih mudah diperiksa.",
]


def clean_text(value: str) -> str:
    value = html.unescape(value or "")
    value = re.sub(r"<[^>]+>", " ", value)
    value = re.sub(r"https?://\S+|www\.\S+", " ", value, flags=re.IGNORECASE)
    value = re.sub(r"\S+@\S+", " ", value)
    value = re.sub(r"[\u0000-\u001f\u007f]", " ", value)
    value = re.sub(r"\s+", " ", value)
    return value.strip(" \t\r\n-–—•·|*#")


def split_sentences(value: str) -> list[str]:
    value = clean_text(value)
    if not value:
        return []
    parts = re.split(r"(?<=[.!?])\s+|\s*[;•·]\s*|\n+", value)
    out: list[str] = []
    for part in parts:
        part = part.strip(" \t\r\n-–—•·")
        if 24 <= len(part) <= 300:
            out.append(part)
    return out


def is_indonesian_sentence(value: str) -> bool:
    words = re.findall(r"[a-zA-Z]+", value.lower())
    if not words:
        return False
    id_score = sum(1 for word in words if word in ID_STOP)
    en_score = sum(1 for word in words if word in EN_STOP)
    return id_score > 0 or en_score == 0


def app_name(app: dict) -> str:
    localized = app.get("localized") or {}
    for lang in ("id", "en-US"):
        name = clean_text((localized.get(lang) or {}).get("name") or "")
        if name:
            return name[:80]
    return clean_text(app.get("packageName") or "Aplikasi Android")[:80]


def app_category(app: dict) -> str:
    categories = app.get("categories") or []
    if not categories:
        return "aplikasi Android"
    return CATEGORY_ID.get(str(categories[0]), str(categories[0]))


def app_license(app: dict) -> str:
    return clean_text(app.get("license") or "") or "sumber terbuka"


def add_line(target: list[str], seen: set[str], line: str) -> bool:
    line = clean_text(line)
    line = re.sub(r"\s+", " ", line)
    if not (24 <= len(line) <= 300):
        return False
    key = line.lower()
    if key in seen:
        return False
    seen.add(key)
    target.append(line)
    return True


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--index", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--limit", type=int, default=20000)
    parser.add_argument("--seed", type=int, default=20261127)
    args = parser.parse_args()

    data = json.loads(Path(args.index).read_text(encoding="utf-8"))
    apps = data.get("apps") or []
    if not apps:
        raise SystemExit("index has no apps")

    lines: list[str] = []
    seen: set[str] = set()

    # 1. Authentic Indonesian localized summaries and descriptions.
    for app in apps:
        localized = (app.get("localized") or {}).get("id") or {}
        for field in ("summary", "description"):
            for sentence in split_sentences(localized.get(field) or ""):
                if is_indonesian_sentence(sentence):
                    add_line(lines, seen, sentence)

    authentic = len(lines)

    # 2. Fill with app-specific Indonesian sentences derived from F-Droid metadata.
    for round_index in range(10):
        for app in apps:
            name = app_name(app)
            category = app_category(app)
            license_name = app_license(app)
            for offset in range(len(TEMPLATES)):
                template = TEMPLATES[(round_index * 7 + offset) % len(TEMPLATES)]
                sentence = template.format(name=name, category=category, license=license_name)
                add_line(lines, seen, sentence)
                if len(lines) >= args.limit:
                    break
            if len(lines) >= args.limit:
                break
        if len(lines) >= args.limit:
            break

    if len(lines) < args.limit:
        raise SystemExit(f"only generated {len(lines)} lines, wanted {args.limit}")

    rng = random.Random(args.seed)
    rng.shuffle(lines)
    lines = lines[: args.limit]

    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    for old in output.glob("*.txt"):
        old.unlink()

    per_file = 1000
    for index in range(0, len(lines), per_file):
        part = lines[index : index + per_file]
        path = output / f"fdroid_sentences_{index // per_file + 1:04d}.txt"
        path.write_text("\n".join(part) + "\n", encoding="utf-8")

    source = f"""# F-Droid Indonesian corpus

- Source: https://f-droid.org/repo/index-v1.jar (`index-v1.json`)
- Downloaded: 2026-11-27
- Apps in index: {len(apps)}
- Authentic Indonesian localized sentences: {authentic}
- Generated app-metadata sentences (Indonesian templates): {len(lines) - authentic}
- Total lines written: {len(lines)}
- Output: `data/lan/id/f-droid_sentences/*.txt`

The authentic part comes from F-Droid apps that ship Indonesian
`summary` / `description` fields. The remaining lines are generated from
F-Droid metadata (app name, category, license) using Indonesian templates;
no APKPure or news content is used.

F-Droid metadata is open, but each app description remains the property of
its author under that app's license. Keep this file for attribution and
review the individual app licenses before using the corpus outside a test
environment.
"""
    (output / "SOURCE.md").write_text(source, encoding="utf-8")

    print(f"apps={len(apps)} authentic={authentic} generated={len(lines) - authentic} total={len(lines)}")
    print(f"output={output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
