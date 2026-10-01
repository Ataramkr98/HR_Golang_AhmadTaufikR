# Simpul HR Backend

Backend modular monolith. API menggunakan `/v1`, UUID, JSON camelCase, waktu UTC RFC3339, tanggal `YYYY-MM-DD`, rupiah integer, dan error `application/problem+json`.

## Komponen

- Go 1.26, Gin, GORM, PostgreSQL, Redis, Asynq, cron, JWT, bcrypt, dan `slog` JSON.
- SQL migration reversible di `migrations/`; aplikasi tidak menjalankan auto-migrate.
- Shared-schema tenancy dengan `organization_id`, filter repository/service, dan PostgreSQL RLS.
- Access token 15 menit hanya untuk memory frontend; refresh token 30 hari di cookie HttpOnly yang dirotasi.
- MFA TOTP/recovery code untuk `hr_admin`, RBAC configurable, audit append-only, AES-256-GCM, idempotency payroll, transactional outbox, dan WebSocket ticket sekali pakai.
- OpenAPI di `openapi/openapi.yaml`; tipe frontend dibuat dengan `npm run generate:api`.

## Menjalankan lokal tanpa Docker

1. Salin `.env.example` menjadi `.env`, lalu isi `DATABASE_URL`, `ACCESS_TOKEN_SECRET`, dan `SEED_ADMIN_PASSWORD`. Untuk selain development, isi `FIELD_ENCRYPTION_KEY` dengan base64 dari tepat 32 byte.
2. Pastikan PostgreSQL dan Redis aktif.
3. Jalankan migrasi dan seed:

   ```powershell
   go run ./cmd/migrate up
   go run ./cmd/seed
   ```

4. Jalankan API dan worker pada terminal terpisah:

   ```powershell
   go run ./cmd/api
   go run ./cmd/worker
   ```

5. Dari `../hr-management`, salin `.env.example` menjadi `.env`, lalu jalankan `npm run dev`.

Seed bersifat idempotent. Akun demo admin adalah `sarah@simpul.co.id`; password selalu dibaca dari `SEED_ADMIN_PASSWORD` dan tidak ditanam di source/UI. Login pertama `hr_admin` meminta enrollment TOTP.

## Menjalankan dengan Compose

```powershell
docker compose up --build -d postgres redis migrate api worker
docker compose --profile demo run --rm seed
```

Default Compose hanya untuk development. Ganti semua secret sebelum lingkungan bersama/production.

## Perintah penting

```powershell
go test ./...
go vet ./...
go run ./cmd/migrate version
go run ./cmd/migrate down 1
```

Health endpoint:

- `GET /health/live`: liveness proses.
- `GET /health/ready`: PostgreSQL wajib; Redis dilaporkan `degraded` agar CRUD tetap hidup dan outbox menunggu pemulihan dependency.
- `GET /openapi.yaml`: kontrak API.

## Worker dan payroll

Worker memproses payroll, rollup absensi, accrual cuti, penjadwalan THR, dan digest notifikasi. Mutasi penting membuat outbox dalam transaksi PostgreSQL yang sama; dispatcher mengirim task ketika Redis tersedia.

Policy payroll bersifat effective-dated dan kalkulasinya pure Go. Rate seed diberi `nonCertified=true`: hasil demo bukan dasar legal untuk pelaporan pajak, BPJS, transfer bank, atau keputusan payroll produksi.

Eksekusi payroll membutuhkan header `Idempotency-Key`; pemanggilan ulang dengan key yang sama mengembalikan job yang sama. Run dikunci sebelum transisi `draft -> queued -> processing -> completed/failed`.

## SSO dan integrasi

Google dan Entra aktif hanya jika client ID/secret tersedia. Daftarkan callback berikut di provider:

- `${PUBLIC_API_URL}/v1/auth/sso/google/callback`
- `${PUBLIC_API_URL}/v1/auth/sso/entra/callback`

Slack memerlukan `config.webhookUrl` dari domain `https://hooks.slack.com/`. Seluruh credential integrasi disimpan terenkripsi dan tidak pernah dikembalikan oleh API.

## Pengujian integrasi

Unit/contract test tidak memerlukan dependency eksternal. Untuk suite PostgreSQL/Redis, gunakan database test terpisah dan set `TEST_DATABASE_URL`/`TEST_REDIS_ADDR`; jangan arahkan test destruktif ke database development/production.

PostgreSQL lokal pada workspace terdeteksi, tetapi smoke test migrasi memerlukan credential database yang valid. Compose menyediakan lingkungan terisolasi saat Docker tersedia.
