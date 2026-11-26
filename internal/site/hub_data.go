package site

// Localized copy for the deterministic hub renderer.
const hubCopyJSON = `{
  "en": {
    "i18n": {
      "page.title_suffix": " — Trusted APK Download Hub",
      "page.description": "Browse curated APK download links, mirror sites and partner resources for %s. Updated regularly to keep installs fast and safe.",
      "nav.heading": "Mirror Sites",
      "side.heading": "Partner Links",
      "main.heading": "%s APK Download Hub",
      "main.subtitle": "Curated free APK resources, official mirrors and trusted partner pages.",
      "intro.heading": "About %s",
      "intro.body": "%s is an aggregated download portal that connects users to verified APK mirrors. Each link is reviewed for availability and safety. Whether you are looking for the official build, a regional mirror, or alternative installers, this hub gives you a single starting point. Bookmark this page and revisit any time the upstream source changes — the latest working URLs are always listed below.",
      "intro.cta": "Featured partners",
      "footer.note": "All trademarks belong to their respective owners. This hub only aggregates third-party links and does not host APK files directly.",
      "common.more": "More",
      "common.visit": "Visit"
    },
    "intro": [
      "Looking for {brand} APK in {year}? This page brings together verified mirror sites, regional installers and partner archives in one place. Every link below is checked for availability and is refreshed when upstream changes. New visitors can pick the closest mirror; returning users can bookmark and come back any time. We list more than {count} curated entries — start with the top section if you want the fastest route, or scroll the partner list if you prefer alternative builds.",
      "The {brand} download experience changes often: official channels rotate, mirrors come and go, and download speeds depend on your region. This {brand} hub solves that by aggregating {count}+ entry points and updating them on a rolling schedule. Whether you need the latest version, a stable older release, or a partner site that hosts a localized build, you can find it here. Each link uses standard HTTP and opens in a new tab so your browser can warn you about anything that looks off.",
      "Trustworthy {brand} APK sources are getting harder to find as the year {year} progresses. We maintain this hub specifically to make discovery painless: more than {count} carefully selected mirror sites and partner pages, each annotated and grouped. Bookmark this page if you reinstall {brand} often — the same URL will always serve the freshest curated list, even after upstream rebrands or domain migrations."
    ],
    "pack": {
      "related": [
        "{brand} Official",
        "{brand} APK",
        "{brand} Login",
        "{brand} Download",
        "{brand} Location",
        "{brand} Discussion"
      ],
      "resource_descriptions": [
        "Use this partner resource when you need a stable {brand} access point with a clear domain trail from {domain}.",
        "This mirror helps visitors compare {brand} availability, download flow and regional access without searching through unrelated pages.",
        "The listing is selected as a supporting route for {brand}, useful when the primary path is slow or unavailable."
      ],
      "article": [
        {
          "heading": "Verified access overview",
          "lead": "{brand} users usually need a page that explains where the current APK access points, mirrors and support references are collected. This resource page keeps those references in one place and highlights a stable partner route such as ",
          "tail": " so visitors can compare the active domain before opening an installer or login page."
        },
        {
          "heading": "Download and mirror context",
          "lead": "A practical APK hub should not only show a bare keyword. It should explain why a mirror exists, whether the route is intended for download, account access or regional availability. For this reason, the current {brand} snapshot includes checked resources like ",
          "tail": " together with related internal pages that describe APK, login and official access topics."
        },
        {
          "heading": "Safety notes for visitors",
          "lead": "Before using any third-party page, visitors should inspect the domain, avoid unexpected permission prompts and keep Android security settings under control. A reference such as ",
          "tail": " is shown as an external resource, while this page remains an index and does not directly host APK files."
        },
        {
          "heading": "Freshness and snapshot date",
          "lead": "This page uses the {date} external-link snapshot, which means the visible partner list stays stable for the same URL and date. If a later snapshot adds more routes, older pages keep their original pool. One selected route in this snapshot is ",
          "tail": " and the rest of the page keeps the same deterministic ordering for repeated crawls."
        },
        {
          "heading": "Related navigation",
          "lead": "For users who need more than one entry point, the related pages below separate official, APK, login and discussion intent. The external resource ",
          "tail": " is included as part of the broader {brand} discovery path, not as a forced redirect."
        }
      ],
      "faq": [
        {
          "q": "What is {brand}?",
          "a": "{brand} is presented here as an APK resource topic with related mirrors, official-style pages and partner references."
        },
        {
          "q": "Does this page host APK files directly?",
          "a": "No. This page aggregates references and external routes; it does not directly host APK files."
        },
        {
          "q": "Why do links stay stable for the same date?",
          "a": "The page uses the {date} snapshot, so repeated visits to the same URL and date keep the same external-link pool."
        },
        {
          "q": "How should visitors check a mirror?",
          "a": "Visitors should inspect the destination domain, avoid suspicious prompts and compare the route with related official or APK pages."
        }
      ]
    },
    "resource": {
      "sentences": [
        "{keyword} is listed as a neutral reference entry for visitors who want to review the destination and its update notes.",
        "The {keyword} entry keeps the destination, short summary and related navigation in one readable place.",
        "Visitors can compare the {keyword} reference with other listed resources before opening the destination."
      ],
      "paragraphs": [
        "The {keyword} entry is a local sample record. The destination domain {domain} is shown as supplied by the managed link data, while the surrounding copy stays deterministic.",
        "This {keyword} profile is generated from the local snapshot so repeated requests keep the same layout and link order.",
        "The {keyword} reference helps test link presentation, local images and multilingual page rendering without storing a downloadable file."
      ]
    },
    "sections": {
      "external": "External links",
      "article": "{brand} Resource Guide",
      "internal_nav": "Recent Link Navigation",
      "resources": "Featured External Resources",
      "related": "Related {brand} Pages",
      "faq": "FAQ"
    }
  },
  "id": {
    "i18n": {
      "page.title_suffix": " — Pusat Unduh APK Tepercaya",
      "page.description": "Kumpulan tautan unduh APK, situs mirror dan halaman mitra untuk %s. Diperbarui rutin agar instalasi tetap cepat dan aman.",
      "nav.heading": "Situs Mirror",
      "side.heading": "Tautan Mitra",
      "main.heading": "Pusat Unduh APK %s",
      "main.subtitle": "Sumber APK gratis pilihan, mirror resmi, dan halaman mitra tepercaya.",
      "intro.heading": "Tentang %s",
      "intro.body": "%s adalah portal unduh agregat yang menghubungkan pengguna ke mirror APK terverifikasi. Setiap tautan diperiksa untuk ketersediaan dan keamanan. Baik Anda mencari build resmi, mirror regional, atau penginstal alternatif, hub ini memberikan satu titik awal. Tandai halaman ini dan kunjungi kembali setiap kali sumber upstream berubah — URL terbaru yang berfungsi selalu tercantum di bawah.",
      "intro.cta": "Mitra unggulan",
      "footer.note": "Semua merek dagang adalah milik pemiliknya masing-masing. Hub ini hanya mengumpulkan tautan pihak ketiga dan tidak meng-host file APK secara langsung.",
      "common.more": "Lainnya",
      "common.visit": "Kunjungi"
    },
    "intro": [
      "Mencari {brand} APK di {year}? Halaman ini menggabungkan situs mirror terverifikasi, penginstal regional, dan arsip mitra dalam satu tempat. Setiap tautan di bawah diperiksa untuk ketersediaan dan diperbarui ketika sumber upstream berubah. Pengunjung baru dapat memilih mirror terdekat; pengguna lama dapat menandai dan kembali kapan saja. Kami mencantumkan lebih dari {count} entri pilihan — mulai dari bagian atas untuk rute tercepat, atau gulir daftar mitra jika ingin build alternatif.",
      "Pengalaman unduh {brand} sering berubah: kanal resmi berputar, mirror datang dan pergi, dan kecepatan unduh tergantung wilayah Anda. Hub {brand} ini mengatasi itu dengan menggabungkan lebih dari {count} titik masuk dan memperbaruinya secara berkala. Apakah Anda membutuhkan versi terbaru, rilis lama yang stabil, atau situs mitra yang meng-host build lokal, Anda dapat menemukannya di sini.",
      "Sumber {brand} APK yang dapat dipercaya semakin sulit ditemukan di tahun {year}. Kami merawat hub ini agar penemuan jadi mudah: lebih dari {count} situs mirror dan halaman mitra yang dipilih dengan cermat, masing-masing diberi anotasi dan dikelompokkan. Tandai halaman ini jika Anda sering memasang ulang {brand} — URL yang sama akan selalu menyajikan daftar pilihan terbaru, bahkan setelah perubahan merek atau migrasi domain di hulu."
    ],
    "pack": {
      "related": [
        "{brand} Resmi",
        "{brand} APK",
        "{brand} Masuk",
        "{brand} Unduh",
        "{brand} Lokasi",
        "{brand} Diskusi"
      ],
      "resource_descriptions": [
        "Gunakan sumber mitra ini saat membutuhkan jalur akses {brand} yang stabil dengan domain jelas dari {domain}.",
        "Mirror ini membantu pengunjung membandingkan ketersediaan {brand}, alur unduhan, dan akses regional.",
        "Daftar ini dipilih sebagai rute pendukung {brand} ketika jalur utama lambat atau tidak tersedia."
      ],
      "article": [
        {
          "heading": "Ringkasan akses terverifikasi",
          "lead": "Pengguna {brand} biasanya membutuhkan halaman yang menjelaskan titik akses APK, mirror, dan referensi bantuan terbaru. Halaman ini mengumpulkan referensi tersebut dan menampilkan rute mitra seperti ",
          "tail": " agar pengunjung dapat memeriksa domain aktif sebelum membuka halaman installer atau login."
        },
        {
          "heading": "Konteks unduhan dan mirror",
          "lead": "Hub APK yang baik tidak hanya menampilkan satu kata kunci. Halaman perlu menjelaskan apakah rute digunakan untuk unduhan, akses akun, atau ketersediaan regional. Snapshot {brand} saat ini menyertakan sumber seperti ",
          "tail": " bersama halaman internal terkait APK, masuk, dan akses resmi."
        },
        {
          "heading": "Catatan keamanan",
          "lead": "Sebelum memakai halaman pihak ketiga, pengunjung sebaiknya memeriksa domain, menghindari izin yang tidak wajar, dan menjaga pengaturan keamanan Android. Referensi seperti ",
          "tail": " ditampilkan sebagai sumber eksternal, sementara halaman ini hanya menjadi indeks."
        },
        {
          "heading": "Tanggal snapshot",
          "lead": "Halaman ini memakai snapshot tautan eksternal {date}. Artinya daftar mitra tetap stabil untuk URL dan tanggal yang sama. Salah satu rute pada snapshot ini adalah ",
          "tail": " dan urutan lainnya tetap deterministik saat dirayapi ulang."
        },
        {
          "heading": "Navigasi terkait",
          "lead": "Jika pengguna membutuhkan lebih dari satu pintu masuk, halaman terkait di bawah memisahkan maksud resmi, APK, masuk, dan diskusi. Sumber eksternal ",
          "tail": " menjadi bagian dari jalur penemuan {brand}, bukan redirect paksa."
        }
      ],
      "faq": [
        {
          "q": "Apa itu {brand}?",
          "a": "{brand} ditampilkan sebagai topik sumber APK dengan mirror, halaman resmi, dan referensi mitra terkait."
        },
        {
          "q": "Apakah halaman ini menyimpan file APK?",
          "a": "Tidak. Halaman ini mengumpulkan referensi dan rute eksternal, bukan menyimpan file APK secara langsung."
        },
        {
          "q": "Mengapa tautan stabil pada tanggal yang sama?",
          "a": "Halaman memakai snapshot {date}, sehingga URL dan tanggal yang sama tetap memakai pool tautan yang sama."
        },
        {
          "q": "Bagaimana memeriksa mirror?",
          "a": "Periksa domain tujuan, hindari permintaan izin mencurigakan, dan bandingkan dengan halaman resmi atau APK terkait."
        }
      ]
    },
    "resource": {
      "sentences": [
        "{keyword} dicatat sebagai entri referensi netral untuk memeriksa tujuan dan catatan pembaruan.",
        "Entri {keyword} menyatukan tujuan, ringkasan singkat, dan navigasi terkait dalam satu tampilan.",
        "Pengunjung dapat membandingkan referensi {keyword} dengan sumber lain sebelum membuka tujuan."
      ],
      "paragraphs": [
        "Entri {keyword} adalah data contoh lokal. Domain tujuan {domain} ditampilkan dari data tautan yang dikelola, sedangkan teks pendamping dibuat deterministik.",
        "Profil {keyword} dibuat dari snapshot lokal agar permintaan berulang mempertahankan susunan halaman dan tautan yang sama.",
        "Referensi {keyword} membantu menguji tampilan tautan, gambar lokal, dan halaman multibahasa tanpa menyimpan berkas unduhan."
      ]
    },
    "sections": {
      "external": "Tautan eksternal",
      "article": "Panduan Sumber {brand}",
      "internal_nav": "Navigasi Link Tujuh Hari",
      "resources": "Sumber Eksternal Pilihan",
      "related": "Halaman {brand} Terkait",
      "faq": "Pertanyaan Umum"
    }
  },
  "th": {
    "i18n": {
      "page.title_suffix": " — ศูนย์รวมดาวน์โหลด APK ที่เชื่อถือได้",
      "page.description": "รวมลิงก์ดาวน์โหลด APK, ไซต์มิเรอร์ และพันธมิตรของ %s อัปเดตสม่ำเสมอเพื่อการติดตั้งที่รวดเร็วและปลอดภัย",
      "nav.heading": "ไซต์มิเรอร์",
      "side.heading": "ลิงก์พันธมิตร",
      "main.heading": "ศูนย์ดาวน์โหลด APK %s",
      "main.subtitle": "แหล่งทรัพยากร APK ฟรีที่คัดสรร มิเรอร์ทางการ และหน้าพันธมิตรที่เชื่อถือได้",
      "intro.heading": "เกี่ยวกับ %s",
      "intro.body": "%s คือพอร์ทัลรวมการดาวน์โหลดที่เชื่อมผู้ใช้กับมิเรอร์ APK ที่ตรวจสอบแล้ว ลิงก์แต่ละรายการได้รับการตรวจสอบเรื่องความพร้อมใช้งานและความปลอดภัย ไม่ว่าคุณกำลังมองหาเวอร์ชันทางการ มิเรอร์ในภูมิภาค หรือผู้ติดตั้งทางเลือก ฮับนี้ให้จุดเริ่มต้นเดียว บันทึกหน้านี้และกลับมาตรวจสอบทุกครั้งที่แหล่งต้นทางเปลี่ยนแปลง URL ที่ใช้งานได้ล่าสุดจะแสดงไว้ด้านล่างเสมอ",
      "intro.cta": "พันธมิตรที่แนะนำ",
      "footer.note": "เครื่องหมายการค้าทั้งหมดเป็นของเจ้าของที่เกี่ยวข้อง ฮับนี้รวบรวมเฉพาะลิงก์ของบุคคลที่สามและไม่ได้โฮสต์ไฟล์ APK โดยตรง",
      "common.more": "เพิ่มเติม",
      "common.visit": "เข้าชม"
    },
    "intro": [
      "กำลังมองหา {brand} APK ในปี {year} อยู่หรือไม่ หน้านี้รวบรวมไซต์มิเรอร์ที่ตรวจสอบแล้ว ตัวติดตั้งระดับภูมิภาค และคลังพันธมิตรไว้ในที่เดียว ทุกลิงก์ด้านล่างถูกตรวจสอบความพร้อมใช้งานและจะรีเฟรชทุกครั้งที่แหล่งต้นทางเปลี่ยน ผู้ใช้ใหม่เลือกมิเรอร์ที่ใกล้ที่สุดได้ ผู้ใช้เดิมบันทึกหน้านี้และกลับมาเมื่อใดก็ได้ เรามีรายการที่คัดสรรกว่า {count} รายการ — เริ่มจากส่วนบนสุดเพื่อเส้นทางที่เร็วที่สุด หรือเลื่อนดูรายการพันธมิตรหากต้องการเวอร์ชันอื่น",
      "ประสบการณ์การดาวน์โหลด {brand} เปลี่ยนแปลงบ่อย ช่องทางทางการหมุนเวียน มิเรอร์มาแล้วก็ไป และความเร็วในการดาวน์โหลดขึ้นกับภูมิภาคของคุณ ฮับ {brand} นี้แก้ปัญหานั้นโดยรวบรวมจุดเริ่มต้นกว่า {count} แห่ง และอัปเดตตามตารางหมุนเวียน ไม่ว่าคุณจะต้องการเวอร์ชันล่าสุด รุ่นเก่าที่เสถียร หรือไซต์พันธมิตรที่โฮสต์เวอร์ชันแปลภาษา คุณสามารถพบได้ที่นี่",
      "แหล่ง {brand} APK ที่เชื่อถือได้หายากขึ้นเรื่อย ๆ ในปี {year} เราดูแลฮับนี้เพื่อให้การค้นหาง่ายขึ้น มีไซต์มิเรอร์และหน้าพันธมิตรที่คัดสรรอย่างพิถีพิถันกว่า {count} แห่ง แต่ละแห่งมีคำอธิบายและจัดกลุ่ม บันทึกหน้านี้หากคุณติดตั้ง {brand} ใหม่บ่อย — URL เดิมจะให้รายชื่อที่สดใหม่เสมอ แม้ต้นทางจะเปลี่ยนชื่อหรือย้ายโดเมน"
    ],
    "pack": {
      "related": [
        "{brand} ทางการ",
        "{brand} APK",
        "{brand} เข้าสู่ระบบ",
        "{brand} ดาวน์โหลด",
        "{brand} พื้นที่ให้บริการ",
        "{brand} พูดคุย"
      ],
      "resource_descriptions": [
        "ใช้แหล่งข้อมูลพาร์ทเนอร์นี้เมื่อต้องการช่องทางเข้าถึง {brand} ที่เสถียรและมีร่องรอยโดเมนชัดเจนจาก {domain}",
        "มิเรอร์นี้ช่วยให้ผู้เข้าชมเปรียบเทียบความพร้อมใช้งานของ {brand} ขั้นตอนดาวน์โหลด และการเข้าถึงตามภูมิภาคได้ง่ายขึ้น",
        "รายการนี้ถูกเลือกเป็นเส้นทางสนับสนุนของ {brand} เมื่อเส้นทางหลักช้าหรือยังไม่พร้อมใช้งาน"
      ],
      "article": [
        {
          "heading": "ภาพรวมช่องทางที่ตรวจสอบแล้ว",
          "lead": "ผู้ใช้ {brand} มักต้องการหน้าที่อธิบายว่าจุดเข้าถึง APK มิเรอร์ และแหล่งอ้างอิงสนับสนุนถูกรวบรวมไว้ที่ใด หน้านี้รวมข้อมูลเหล่านั้นไว้ในที่เดียวและแสดงเส้นทางพาร์ทเนอร์ที่เสถียร เช่น ",
          "tail": " เพื่อให้ผู้เข้าชมตรวจสอบโดเมนก่อนเปิดหน้า installer หรือเข้าสู่ระบบ"
        },
        {
          "heading": "บริบทการดาวน์โหลดและมิเรอร์",
          "lead": "ฮับ APK ที่ดีไม่ควรมีเพียงคำหลักเดียว แต่ควรอธิบายว่าเส้นทางนั้นใช้สำหรับดาวน์โหลด เข้าถึงบัญชี หรือดูความพร้อมใช้งานตามภูมิภาค Snapshot ปัจจุบันของ {brand} จึงรวมแหล่งข้อมูลที่ตรวจสอบได้ เช่น ",
          "tail": " พร้อมหน้าภายในที่เกี่ยวข้องกับ APK การเข้าสู่ระบบ และช่องทางทางการ"
        },
        {
          "heading": "ข้อควรระวังด้านความปลอดภัย",
          "lead": "ก่อนใช้หน้าภายนอก ผู้เข้าชมควรตรวจสอบโดเมน หลีกเลี่ยงคำขอสิทธิ์ที่ผิดปกติ และควบคุมการตั้งค่าความปลอดภัยของ Android แหล่งอ้างอิงอย่าง ",
          "tail": " จะแสดงเป็นทรัพยากรภายนอก ขณะที่หน้านี้ทำหน้าที่เป็นดัชนีเท่านั้น"
        },
        {
          "heading": "วันที่ Snapshot และความสดใหม่",
          "lead": "หน้านี้ใช้ snapshot ลิงก์ภายนอกวันที่ {date} ดังนั้นรายชื่อพาร์ทเนอร์ที่แสดงจะคงที่สำหรับ URL และวันที่เดียวกัน หาก snapshot ใหม่เพิ่มเส้นทางมากขึ้น หน้าเก่าจะยังเก็บ pool เดิมไว้ เส้นทางหนึ่งใน snapshot นี้คือ ",
          "tail": " และส่วนที่เหลือของหน้าจะรักษาลำดับแบบ deterministic สำหรับการ crawl ซ้ำ"
        },
        {
          "heading": "การนำทางที่เกี่ยวข้อง",
          "lead": "สำหรับผู้ใช้ที่ต้องการมากกว่าหนึ่งทางเข้า หน้าที่เกี่ยวข้องด้านล่างจะแยกเจตนาเรื่องทางการ APK เข้าสู่ระบบ และการพูดคุย ทรัพยากรภายนอก ",
          "tail": " เป็นส่วนหนึ่งของเส้นทางค้นพบ {brand} ไม่ใช่การบังคับ redirect"
        }
      ],
      "faq": [
        {
          "q": "{brand} คืออะไร?",
          "a": "{brand} ถูกนำเสนอเป็นหัวข้อทรัพยากร APK พร้อมมิเรอร์ หน้าสไตล์ทางการ และแหล่งอ้างอิงพาร์ทเนอร์ที่เกี่ยวข้อง"
        },
        {
          "q": "หน้านี้โฮสต์ไฟล์ APK โดยตรงหรือไม่?",
          "a": "ไม่ หน้านี้รวบรวมแหล่งอ้างอิงและเส้นทางภายนอก ไม่ได้โฮสต์ไฟล์ APK โดยตรง"
        },
        {
          "q": "ทำไมลิงก์จึงคงที่ในวันที่เดียวกัน?",
          "a": "หน้าใช้ snapshot วันที่ {date} ดังนั้น URL และวันที่เดียวกันจะใช้ pool ลิงก์ภายนอกชุดเดิม"
        },
        {
          "q": "ควรตรวจสอบมิเรอร์อย่างไร?",
          "a": "ควรตรวจสอบโดเมนปลายทาง หลีกเลี่ยงคำขอสิทธิ์ที่น่าสงสัย และเปรียบเทียบกับหน้าทางการหรือหน้า APK ที่เกี่ยวข้อง"
        }
      ]
    },
    "resource": {
      "sentences": [
        "{keyword} ถูกบันทึกเป็นรายการอ้างอิงกลางสำหรับตรวจสอบปลายทางและบันทึกการอัปเดต",
        "รายการ {keyword} รวมปลายทาง สรุปสั้น และการนำทางที่เกี่ยวข้องไว้ในหน้าเดียว",
        "ผู้เข้าชมสามารถเปรียบเทียบรายการ {keyword} กับแหล่งอื่นก่อนเปิดปลายทาง"
      ],
      "paragraphs": [
        "รายการ {keyword} เป็นข้อมูลตัวอย่างภายใน ชื่อโดเมนปลายทาง {domain} มาจากข้อมูลลิงก์ที่จัดการไว้ ส่วนข้อความประกอบสร้างอย่างคงที่",
        "โปรไฟล์ {keyword} สร้างจาก snapshot ภายใน เพื่อให้การเข้าชมซ้ำคงลำดับหน้าและลิงก์เดิม",
        "รายการ {keyword} ใช้ตรวจสอบการแสดงลิงก์ รูปภาพภายใน และหน้าหลายภาษา โดยไม่เก็บไฟล์ดาวน์โหลด"
      ]
    },
    "sections": {
      "external": "ลิงก์ภายนอก",
      "article": "คู่มือแหล่งข้อมูล {brand}",
      "internal_nav": "ลิงก์ภายใน 7 วันล่าสุด",
      "resources": "แหล่งข้อมูลภายนอกที่แนะนำ",
      "related": "หน้าที่เกี่ยวข้องกับ {brand}",
      "faq": "คำถามที่พบบ่อย"
    }
  },
  "vn": {
    "i18n": {
      "page.title_suffix": " - Trung tam tai APK dang tin cay",
      "page.description": "Duyet cac lien ket tai APK, site mirror va nguon doi tac duoc chon loc cho %s. Noi dung duoc cap nhat dinh ky de giu qua trinh cai dat nhanh va an toan.",
      "nav.heading": "Site mirror",
      "side.heading": "Lien ket doi tac",
      "main.heading": "Trung tam tai APK %s",
      "main.subtitle": "Nguon APK mien phi duoc chon loc, mirror kieu chinh thuc va trang doi tac dang tin cay.",
      "intro.heading": "Gioi thieu %s",
      "intro.body": "%s la cong thong tin tong hop tai ve, ket noi nguoi dung voi cac mirror APK da duoc kiem tra. Moi lien ket duoc xem xet ve tinh kha dung va an toan. Khi ban can ban chinh thuc, mirror theo khu vuc hoac bo cai thay the, hub nay cung cap mot diem bat dau ro rang.",
      "intro.cta": "Doi tac noi bat",
      "footer.note": "Tat ca nhan hieu thuoc ve chu so huu tuong ung. Hub nay chi tong hop lien ket ben thu ba va khong luu tru tep APK truc tiep.",
      "common.more": "Them",
      "common.visit": "Truy cap"
    },
    "intro": [
      "Ban dang tim APK {brand} trong nam {year}? Trang nay gom cac site mirror da kiem tra, bo cai theo khu vuc va kho doi tac vao mot noi. Moi lien ket ben duoi duoc kiem tra tinh kha dung va lam moi khi nguon thay doi. Nguoi moi co the chon mirror gan nhat, con nguoi truy cap lai co the danh dau trang de quay lai bat cu luc nao. Chung toi liet ke hon {count} muc da chon loc, hay bat dau tu phan dau neu can duong nhanh nhat.",
      "Trai nghiem tai {brand} thay doi thuong xuyen: kenh chinh thuc luan chuyen, mirror co the bien mat va toc do tai phu thuoc khu vuc. Hub {brand} nay giai quyet viec do bang cach tong hop hon {count} diem vao va cap nhat theo lich cuon. Du ban can ban moi nhat, ban cu on dinh hay site doi tac co ban ban dia phuong hoa, ban co the tim thay tai day.",
      "Nguon APK {brand} dang tin cay ngay cang kho tim khi nam {year} tiep dien. Chung toi duy tri hub nay de viec kham pha don gian hon: hon {count} mirror va trang doi tac duoc chon loc can than, moi muc deu duoc sap xep de de kiem tra. Hay danh dau trang neu ban thuong cai lai {brand}, cung mot URL van co the phuc vu danh sach duoc chon loc moi nhat."
    ],
    "pack": {
      "related": [
        "{brand} Chinh thuc",
        "{brand} APK",
        "{brand} Dang nhap",
        "{brand} Tai ve",
        "{brand} Khu vuc",
        "{brand} Thao luan"
      ],
      "resource_descriptions": [
        "Dung nguon doi tac nay khi can mot diem truy cap {brand} on dinh voi dau vet ten mien ro rang tu {domain}.",
        "Mirror nay giup nguoi truy cap so sanh tinh kha dung cua {brand}, luong tai ve va kha nang truy cap theo khu vuc.",
        "Danh sach nay duoc chon lam tuyen ho tro cho {brand} khi duong dan chinh cham hoac tam thoi khong kha dung."
      ],
      "article": [
        {
          "heading": "Tong quan truy cap da kiem tra",
          "lead": "Nguoi dung {brand} thuong can mot trang giai thich noi tap hop cac diem truy cap APK, mirror va tai lieu ho tro hien tai. Trang nay giu cac tham chieu do o mot noi va hien thi mot tuyen doi tac on dinh nhu ",
          "tail": " de khach truy cap co the kiem tra ten mien truoc khi mo trang cai dat hoac dang nhap."
        },
        {
          "heading": "Ngu canh tai ve va mirror",
          "lead": "Mot hub APK thuc te khong chi nen hien mot tu khoa. Trang can giai thich vi sao mirror ton tai va tuyen do phuc vu tai ve, truy cap tai khoan hay kha dung theo khu vuc. Vi vay snapshot {brand} hien tai gom cac nguon da kiem tra nhu ",
          "tail": " cung voi cac trang noi bo ve APK, dang nhap va truy cap chinh thuc."
        },
        {
          "heading": "Ghi chu an toan cho nguoi truy cap",
          "lead": "Truoc khi dung bat ky trang ben thu ba nao, nguoi truy cap nen kiem tra ten mien, tranh yeu cau quyen bat thuong va giu cai dat bao mat Android trong tam kiem soat. Tham chieu nhu ",
          "tail": " duoc hien thi nhu mot nguon ben ngoai, con trang nay chi la muc luc."
        },
        {
          "heading": "Ngay snapshot va do moi",
          "lead": "Trang nay dung snapshot lien ket ben ngoai ngay {date}, nghia la danh sach doi tac hien thi se on dinh voi cung URL va ngay. Neu snapshot moi them nhieu tuyen hon, cac trang cu van giu pool ban dau. Mot tuyen duoc chon trong snapshot nay la ",
          "tail": " va phan con lai cua trang giu thu tu deterministic cho cac lan crawl lap lai."
        },
        {
          "heading": "Dieu huong lien quan",
          "lead": "Voi nguoi dung can hon mot diem vao, cac trang lien quan ben duoi tach ro muc dich chinh thuc, APK, dang nhap va thao luan. Nguon ben ngoai ",
          "tail": " la mot phan cua duong kham pha {brand}, khong phai redirect bat buoc."
        }
      ],
      "faq": [
        {
          "q": "{brand} la gi?",
          "a": "{brand} duoc trinh bay nhu mot chu de tai nguyen APK voi mirror, trang kieu chinh thuc va tham chieu doi tac lien quan."
        },
        {
          "q": "Trang nay co luu tru tep APK truc tiep khong?",
          "a": "Khong. Trang nay tong hop tham chieu va tuyen ben ngoai, khong luu tru tep APK truc tiep."
        },
        {
          "q": "Vi sao lien ket on dinh trong cung mot ngay?",
          "a": "Trang dung snapshot {date}, nen cung URL va cung ngay se giu nguyen pool lien ket ben ngoai."
        },
        {
          "q": "Nguoi truy cap nen kiem tra mirror nhu the nao?",
          "a": "Hay kiem tra ten mien dich, tranh cac loi nhac dang nghi va so sanh voi trang chinh thuc hoac trang APK lien quan."
        }
      ]
    },
    "resource": {
      "sentences": [
        "{keyword} duoc ghi lai nhu mot muc tham chieu trung lap de kiem tra dich den va ghi chu cap nhat.",
        "Muc {keyword} gom dich den, tom tat ngan va dieu huong lien quan trong mot bo cuc de doc.",
        "Nguoi xem co the so sanh tham chieu {keyword} voi cac nguon khac truoc khi mo dich den."
      ],
      "paragraphs": [
        "Muc {keyword} la du lieu mau cuc bo. Ten mien dich den {domain} duoc lay tu du lieu lien ket duoc quan ly, con noi dung phu duoc tao on dinh.",
        "Ho so {keyword} duoc tao tu snapshot cuc bo de cac lan truy cap lap lai giu nguyen thu tu trang va lien ket.",
        "Tham chieu {keyword} dung de kiem tra cach hien thi lien ket, hinh anh cuc bo va trang da ngon ngu ma khong luu tep tai xuong."
      ]
    },
    "sections": {
      "external": "Lien ket ben ngoai",
      "article": "Huong dan tai nguyen {brand}",
      "internal_nav": "Dieu huong link bay ngay",
      "resources": "Nguon ben ngoai noi bat",
      "related": "Trang lien quan den {brand}",
      "faq": "Cau hoi thuong gap"
    }
  }
}`
