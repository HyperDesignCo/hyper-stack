# Hyper Stack — موجز المشروع لـ Claude Code

> اقرأ هذا الملف كاملاً قبل كتابة أي كود. ابدأ بالمرحلة 0 (Spike)، وارجع لي بتقرير قبل الانتقال للمرحلة 1.

## 1. ما الذي نبنيه

**Hyper Stack** أداة داخلية لوكالة Hyper Design. تعطي فريق التطوير (حوالي 15 شخصاً، 5 على macOS والباقي على Windows) بيئة Laravel محلية موحدة، بدون تثبيت أي برنامج يدوياً.

الأداة سطر أوامر واحد اسمه `hyper`. يقرأ ملف `hyper.json` من جذر أي مشروع، ثم يجهّز كل ما يحتاجه المشروع: PHP بالإصدار المطلوب، قاعدة البيانات، الدومين المحلي، HTTPS، ملف `.env`، وأوامر الإعداد الأولى.

**الهدف القابل للقياس:** جهاز نظيف (Windows أو Mac) يصل من الصفر إلى مشروع يعمل في المتصفح في أقل من 10 دقائق، بأمرين فقط:

```bash
hyper install
hyper clone git@github.com:hyperdesign/bazooka.git
```

## 2. القرارات المعتمدة

| القرار | الاختيار | السبب |
| --- | --- | --- |
| لغة الأداة | **Go** (1.22+) | ملف تنفيذي واحد لكل نظام، ولا يحتاج PHP مثبتاً مسبقاً (لأن `hyper install` هو من يثبّت PHP) |
| مكتبة CLI | `spf13/cobra` | المعيار في Go، ويدعم الأوامر الفرعية والـ help |
| طبقة التشغيل | **FlyEnv** عبر واجهة `Runtime` قابلة للاستبدال | FlyEnv مفتوح المصدر (BSD) ويشغّل PHP وNginx وMySQL كـ binaries أصلية على Windows وmacOS |
| سيرفر الويب | Nginx (المضمّن في FlyEnv) | مطابق لسيرفرات الإنتاج |
| قاعدة البيانات | MySQL 8 | مطابق للإنتاج |
| HTTPS المحلي | توليد CA محلي وشهادات بأسلوب mkcert (مكتبة Go) | بدون اعتماد على أداة خارجية |
| المنصات المستهدفة | Windows x64، macOS arm64، macOS amd64 | أجهزة الفريق الحالية. Linux خارج النطاق |

**قاعدة معمارية أساسية:** لا يعتمد أي كود في `hyper` على FlyEnv مباشرة إلا داخل `internal/runtime/flyenv`. لو توقف FlyEnv أو لم يناسبنا، نكتب `Runtime` بديلاً (binaries مباشرة أو Laravel Herd) دون لمس باقي الكود أو ملفات `hyper.json` في المشاريع.

## 3. المرحلة 0 — Spike (ابدأ هنا)

**لا تكتب كود الإنتاج قبل إنهاء هذه المرحلة.** المطلوب تقرير مكتوب (`docs/spike-flyenv.md`) يجيب عن:

1. كيف يثبّت FlyEnv الخدمات؟ وأين يخزن الـ binaries على Windows وmacOS؟
2. هل يوفر CLI أو API أو ملف إعداد يمكن التحكم به برمجياً؟ أم أن التحكم من الواجهة فقط؟
3. كيف يدير المواقع (vhosts) والدومينات المحلية؟ وأين تُحفظ ملفات إعداد Nginx؟
4. هل يمكن تشغيل أكثر من إصدار PHP في نفس الوقت (PHP-FPM لكل إصدار على منفذ مختلف)؟
5. كيف يتعامل مع ملف hosts والصلاحيات (UAC على Windows، sudo على macOS)؟

**بناءً على التقرير، اقترح أحد خيارين:**

- **(أ) Adapter فوق FlyEnv:** إذا كان قابلاً للتحكم برمجياً بشكل موثوق.
- **(ب) Runtime مباشر:** `hyper` يحمّل binaries ثابتة (PHP من static-php-cli، Nginx، MySQL) ويديرها بنفسه، وFlyEnv يصبح اختيارياً.

انتظر موافقتي على الخيار قبل بدء المرحلة 1.

## 4. هيكل المستودع المقترح

```
hyper-stack/
├── cmd/hyper/main.go
├── internal/
│   ├── cli/              # تعريف أوامر cobra فقط، بدون منطق
│   ├── project/          # قراءة hyper.json والتحقق منه
│   ├── runtime/          # واجهة Runtime
│   │   ├── runtime.go
│   │   ├── flyenv/       # Adapter لـ FlyEnv
│   │   └── direct/       # (إن احتجنا) إدارة binaries مباشرة
│   ├── services/         # mysql, redis, mailpit, phpmyadmin
│   ├── hosts/            # تعديل ملف hosts بأمان
│   ├── certs/            # CA محلي وشهادات *.test
│   ├── nginx/            # توليد vhosts من قوالب
│   ├── envfile/          # تعديل .env بدون تخريب القيم الأخرى
│   ├── doctor/           # فحوصات البيئة
│   └── config/           # إعدادات الوكالة (stack-config)
├── templates/            # nginx vhost, php.ini, my.cnf
├── schema/hyper.schema.json
├── docs/
├── scripts/              # build و release
└── .github/workflows/    # CI يبني للمنصات الثلاث ويشغّل الاختبارات
```

## 5. واجهة Runtime

```go
type Runtime interface {
    Name() string
    EnsureInstalled(ctx context.Context, spec StackSpec) error
    EnsurePHP(ctx context.Context, version string, extensions []string) (PHPInstall, error)
    StartService(ctx context.Context, svc Service) error
    StopService(ctx context.Context, svc Service) error
    ServiceStatus(ctx context.Context, svc Service) (Status, error)
    RegisterSite(ctx context.Context, site Site) error   // vhost + PHP-FPM
    UnregisterSite(ctx context.Context, name string) error
}
```

عدّل التوقيعات حسب نتائج الـ Spike، لكن حافظ على المبدأ: كل ما يخص FlyEnv يبقى خلف هذه الواجهة.

## 6. ملف hyper.json

يوضع في جذر كل مشروع ويُرفع على Git. أنشئ JSON Schema له في `schema/hyper.schema.json`، وتحقق منه قبل أي تنفيذ مع رسائل خطأ واضحة تشير إلى الحقل المخطئ.

```json
{
  "name": "bazooka",
  "domain": "bazooka.test",
  "php": "8.3",
  "extensions": ["redis", "gd", "intl", "zip"],
  "database": {
    "engine": "mysql",
    "name": "bazooka_local",
    "seed": true
  },
  "services": ["redis", "mailpit"],
  "node": "20",
  "setup": [
    "composer install",
    "npm ci && npm run build",
    "php artisan key:generate",
    "php artisan migrate --seed",
    "php artisan storage:link"
  ],
  "workers": ["php artisan queue:work"]
}
```

| الحقل | النوع | إلزامي | الافتراضي / القيود |
| --- | --- | --- | --- |
| `name` | string | نعم | `^[a-z0-9-]+$` |
| `domain` | string | لا | `{name}.test`، ويجب أن ينتهي بـ `.test` |
| `php` | string | نعم | `8.2` أو `8.3` أو `8.4` |
| `extensions` | string[] | لا | إضافات فوق الأساسية التي يحتاجها Laravel |
| `database.engine` | string | نعم | `mysql` فقط في الـ MVP |
| `database.name` | string | نعم | `^[a-z0-9_]+$` |
| `database.seed` | bool | لا | `false` |
| `services` | string[] | لا | `redis`، `mailpit` |
| `node` | string | لا | `20` |
| `setup` | string[] | لا | تُنفذ مرة واحدة فقط عند أول `up` |
| `workers` | string[] | لا | تعمل في الخلفية مع `up` وتتوقف مع `down` |

## 7. الأوامر

### MVP (المرحلة 1)

| الأمر | السلوك المطلوب |
| --- | --- |
| `hyper install` | يثبت طبقة التشغيل وPHP 8.3 وMySQL 8 وNginx وComposer وNode 20 وphpMyAdmin. ينشئ CA محلياً ويضيفه لمخزن الشهادات الموثوقة. يطلب صلاحيات المسؤول **مرة واحدة فقط** هنا. يجب أن يكون **idempotent**: تشغيله مرتين لا يكسر شيئاً |
| `hyper up` | يقرأ `hyper.json` ويتحقق منه، ثم يتأكد من إصدار PHP وإضافاته، ويشغّل الخدمات، وينشئ قاعدة البيانات إن لم تكن موجودة، ويسجل الدومين في hosts، ويولّد شهادة وvhost، ويحدّث `.env`، وينفذ `setup` في أول مرة فقط، ويشغّل `workers`، ثم يطبع رابط الموقع |
| `hyper down` | يوقف workers الخاصة بالمشروع. الخدمات المشتركة (MySQL) تبقى تعمل إلا مع `--all` |
| `hyper db:open` | يفتح phpMyAdmin في المتصفح على قاعدة المشروع مباشرة |
| `hyper db:import <file.sql[.gz]>` | يستورد ملف SQL في قاعدة المشروع، مع شريط تقدم للملفات الكبيرة |
| `hyper doctor` | يفحص: المنافذ (80، 443، 3306) ومن يستخدمها، وإصدارات الخدمات، وإضافات PHP المطلوبة، وصلاحية الشهادة، وسجلات hosts. لكل مشكلة: وصف واضح + الحل المقترح. **لا يصلح شيئاً تلقائياً** إلا مع `--fix` |
| `hyper status` | جدول بالمشاريع المسجلة وحالة كل خدمة |

### المرحلة 2

| الأمر | السلوك |
| --- | --- |
| `hyper clone <repo>` | `git clone` ثم `hyper up` |
| `hyper update` | يسحب أحدث إعدادات الوكالة من مستودع `hyperdesign/stack-config` |
| `hyper share` | رابط مؤقت للموقع المحلي لعرضه على عميل (Cloudflare Tunnel أو بديل مجاني) |
| `hyper php <version>` | يغيّر إصدار PHP للمشروع الحالي ويحدّث `hyper.json` |
| واجهة System Tray | قائمة بالمشاريع وحالتها، أزرار up/down، وروابط سريعة للموقع وphpMyAdmin وMailpit والسجلات. اقترح التقنية (مثل Wails أو `fyne.io/systray`) بعد المرحلة 1 |

## 8. قواعد التنفيذ الحساسة

### ملف .env
- لا تعيد كتابة الملف بالكامل أبداً. عدّل هذه المفاتيح فقط، واحتفظ بالترتيب والتعليقات وباقي القيم:
  `APP_URL`، `DB_CONNECTION`، `DB_HOST`، `DB_PORT`، `DB_DATABASE`، `DB_USERNAME`، `DB_PASSWORD`، `REDIS_HOST`، `REDIS_PORT`، `MAIL_MAILER`، `MAIL_HOST`، `MAIL_PORT`
- إذا لم يوجد `.env`، انسخه من `.env.example` أولاً.
- قبل التعديل، احفظ نسخة احتياطية في `.env.hyper-backup`.

### ملف hosts
- كل السطور التي تضيفها الأداة تكون داخل كتلة محددة:
  ```
  # >>> hyper-stack >>>
  127.0.0.1 bazooka.test
  # <<< hyper-stack <<<
  ```
- لا تلمس أي سطر خارج هذه الكتلة. خذ نسخة احتياطية قبل كل تعديل.

### الصلاحيات
- Windows: اطلب الرفع (UAC) فقط للعمليات التي تحتاجه (hosts، الشهادات، المنافذ < 1024)، عبر عملية مساعدة منفصلة، وليس بتشغيل الأداة كلها كمسؤول.
- macOS: نفس المبدأ عبر `sudo` لعمليات محددة فقط.

### المسارات وفروق المنصات
- استخدم `filepath` دائماً، ولا تكتب `/` أو `\` يدوياً.
- مكان بيانات الأداة: `%LOCALAPPDATA%\HyperStack` على Windows، و`~/Library/Application Support/HyperStack` على macOS.
- انتبه لفرق حساسية حالة الأحرف في أسماء الملفات بين المنصات والإنتاج (Linux). `hyper doctor` يحذّر من imports مختلفة الحالة إن أمكن.

### إعدادات الوكالة الموحدة
- `templates/php.ini` و`templates/my.cnf` يجب أن تطابق قيم سيرفرات الإنتاج. ضع قيماً مبدئية معقولة لـ Laravel (`memory_limit=512M`، `upload_max_filesize=64M`، `post_max_size=64M`، `max_execution_time=120`، `sql_mode` الافتراضي في MySQL 8، `innodb_buffer_pool_size` مناسب لجهاز تطوير)، **وعلّمها بتعليق `# TODO: confirm with production`** لأراجعها.

### المنافذ
- عند تعارض المنفذ 80 أو 443 أو 3306 (Laragon، XAMPP، IIS، خدمة MySQL مثبتة)، لا توقف أي برنامج تلقائياً. اعرض من يستخدم المنفذ واقترح الحل.

## 9. الاختبارات والجودة

- اختبارات وحدة لكل حزمة في `internal/`، خصوصاً: `project` (التحقق من hyper.json)، و`envfile`، و`hosts`، و`nginx` (توليد القوالب). هذه الحزم يجب أن تعمل على نسخ مؤقتة من الملفات في الاختبارات، **وليس ملفات النظام الحقيقية**.
- CI على GitHub Actions يشغّل الاختبارات على `windows-latest` و`macos-latest`، ويبني الملفات التنفيذية الثلاثة.
- `golangci-lint` بدون تحذيرات.
- رسائل المستخدم في الأداة بالإنجليزية (لتطابق المصطلحات التقنية)، مختصرة، وكل خطأ يتضمن ما حدث وكيف يُحل.
- أضف `--verbose` لكل أمر، وسجّل كل العمليات في ملف log داخل مجلد بيانات الأداة.

## 10. معايير قبول الـ MVP

- [ ] جهاز Windows نظيف وجهاز macOS (Apple Silicon) نظيف يصلان إلى مشروع Laravel يعمل على `https://project.test` في أقل من 10 دقائق
- [ ] مشروعان بإصداري PHP مختلفين (8.2 و8.3) يعملان في نفس الوقت على نفس الجهاز
- [ ] `hyper db:open` يفتح phpMyAdmin على قاعدة المشروع مباشرة
- [ ] `hyper doctor` يكتشف تعارض المنفذ 80 مع Laragon ويقترح الحل
- [ ] تشغيل `hyper install` و`hyper up` مرتين متتاليتين لا يغيّر شيئاً ولا يُظهر أخطاء
- [ ] `.env` الموجود مسبقاً يحتفظ بكل قيمه غير المدارة بعد `hyper up`
- [ ] ثلاثة مشاريع حقيقية للوكالة تعمل بملف `hyper.json` دون تعديل يدوي

## 11. خطة العمل

| المرحلة | المدة | المخرجات |
| --- | --- | --- |
| 0. Spike | 2–3 أيام | `docs/spike-flyenv.md` + توصية (أ) أو (ب) |
| 1. MVP | الأسبوعان 2–3 | الأوامر في القسم 7 (MVP) + الاختبارات + CI |
| 2. التجربة والواجهة | الأسبوعان 4–5 | أوامر المرحلة 2 + الواجهة + تجربة على 3 مشاريع مع 4 مطورين |
| 3. الإطلاق | الأسبوع 6 | دليل استخدام في `docs/` + ملفات release موقّعة |

## 12. ممنوعات

- لا Docker ولا WSL.
- لا تعدّل ملفات النظام (hosts، الشهادات، الخدمات) من داخل الاختبارات.
- لا توقف أو تحذف أي برنامج مثبت لدى المستخدم.
- لا تخزن كلمات مرور في ملفات نصية مكشوفة. كلمة مرور MySQL المحلية تُولّد عند التثبيت وتُحفظ في مخزن أسرار النظام (Credential Manager على Windows، Keychain على macOS).
- لا تبدأ مرحلة قبل موافقتي على نتائج المرحلة السابقة.

## 13. أول ما أنتظره منك

1. أنشئ هيكل المستودع (القسم 4) مع `go.mod` وأمر `hyper --version` يعمل.
2. نفّذ الـ Spike (القسم 3) واكتب التقرير.
3. أرسل لي التوصية (أ) أو (ب) مع أسبابها، وأي تعديل تقترحه على واجهة `Runtime`.
