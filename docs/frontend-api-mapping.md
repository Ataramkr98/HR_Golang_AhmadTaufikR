# Pemetaan Frontend ke API Simpul HR

Semua path di bawah memakai base `/v1`. Guard router melakukan `POST /auth/refresh`; access token disimpan di memory Pinia. Mutasi menginvalidasi query terkait, menampilkan error server, dan tidak menyimpan state domain di `localStorage`.

| Route / area UI | Aksi utama | Endpoint | Permission / scope | Transisi state |
|---|---|---|---|---|
| `/login` | Login/password, pilih workspace | `POST /auth/login` | Publik; membership wajib | credential -> workspace/MFA/session |
| `/login` | Enrollment/verifikasi MFA | `POST /auth/mfa/enroll`, `/confirm`, `/verify` | challenge 5 menit; `hr_admin` wajib | challenge -> enrolled/verified -> session |
| `/login` | Google/Entra SSO | `GET /auth/sso/{provider}/start`, `/callback` | Adapter hanya bila configured | OAuth code -> refresh session |
| Header/router | Bootstrap, profil sesi, logout | `POST /auth/refresh`, `GET /me`, `POST /auth/logout` | Authenticated | refresh rotation; logout revokes cookie |
| `/dashboard` | Ringkasan dan dismiss perhatian | `GET /dashboard/summary`, `POST /dashboard/attention/{key}/dismiss` | Authenticated | source metrics -> cached summary |
| Header | Global search | `GET /search?q=` | Hasil disaring permission/tenant | query -> employees/jobs |
| Header | Daftar/baca/hapus notifikasi | `GET /notifications`, `POST /notifications/{id}/read`, `DELETE /notifications/{id}` | Milik user/tenant | unread -> read/dismissed |
| Semua route | Realtime connection | `POST /realtime/tickets`, `GET /ws?ticket=` | Ticket 60 detik, sekali pakai | disconnected -> connecting -> connected/retry |
| `/induk` | Cari/list/tambah/edit karyawan | `GET/POST /employees`, `GET/PATCH /employees/{id}` | `employees.view/manage`; manager direct reports | form -> persisted -> cache invalidated |
| `/induk` | Departemen/posisi | `GET/POST /departments`, `DELETE /departments/{id}`, `GET/POST /positions` | `employees.manage` untuk mutasi | create/delete -> directory refresh |
| `/induk/org-chart` | Hierarki organisasi | `GET /org-chart` | `employees.view`, tenant-scoped | employee/manager changes -> recalculated tree |
| `/hadir` | Riwayat, clock-in/out, koreksi | `GET/POST /attendance`, `PATCH /attendance/{id}`, `POST /attendance/clock-in`, `/clock-out` | Self; `attendance.manage` untuk manual/edit | none -> clocked_in -> completed; edit audited |
| `/hadir/shift` | Lihat/assign shift | `GET/PUT /shift-assignments` | `attendance.view/manage` | assignment -> persisted schedule |
| `/cuti` | Tipe, saldo, pengajuan | `GET /leave-types`, `/leave-balances`, `GET/POST /leave-requests` | Self; list scoped by service | draft UI -> pending |
| `/cuti` | Approval/rejection | `POST /leave-requests/{id}/approve|reject` | `leave.approve`; approver scope checked | pending -> approved/rejected |
| `/gaji` | Run, payslip, execute | `GET/POST /payroll-runs`, `GET /payroll-runs/{id}`, `GET /payslips`, `POST /payroll-runs/{id}/execute` | `payroll.view/run`; payslip self unless finance/admin | draft -> queued -> processing -> completed/failed |
| `/rekrut` | Lowongan/kandidat | `GET/POST/PATCH /job-postings`, `GET/POST /candidates` | `recruitment.view/manage` | candidate created in applied |
| `/rekrut` | Pindah pipeline | `PATCH /candidates/{id}/stage` | `recruitment.manage` | stage A -> B + immutable history |
| `/tumbuh` | Siklus, goal, review | `GET/POST /performance-cycles`, `GET/POST/PATCH /goals`, `GET/POST /reviews` | `performance.view/manage`; employee scope | goal 0..100; review rating 1..5 |
| `/wawasan` | Analitik workforce | `GET /analytics/workforce` | `analytics.view` | domain data -> live aggregation/cache |
| `/pengaturan` | Profil organisasi | `GET/PATCH /organization` | `settings.manage` untuk mutasi | saved -> session/query refresh |
| `/pengaturan` | Role/permission | `GET /roles`, `/permissions`, `PUT /roles/{id}/permissions` | `settings.manage` | permission set replaced; session berikutnya berubah |
| `/pengaturan` | Provider integrasi | `GET /integrations`, `PATCH /integrations/{provider}` | `settings.manage` | disabled/configured -> enabled/disabled; audited |
| `/profil` | Profil dan password | `GET/PATCH /me/profile`, `POST /me/password` | Self | profile updated; password change revokes sessions |
| `/bantuan` | FAQ dan tiket | `GET /faqs`, `GET/POST /support-tickets` | Authenticated; user sees own, admin sees tenant | new -> open -> in_progress/resolved |
| Drawer chat | Conversation/message | `GET/POST /conversations`, `GET/POST /conversations/{id}/messages` | Participant + tenant | persisted message -> Redis realtime event |

Nilai uang adalah integer rupiah. Tanggal dikirim `YYYY-MM-DD`; timestamp adalah UTC RFC3339. Endpoint list menerima `page`/`pageSize` serta filter domain dan mengembalikan `meta.total` bila dipaginasi.
