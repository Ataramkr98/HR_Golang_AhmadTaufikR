CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE organizations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), slug text NOT NULL UNIQUE,
    name text NOT NULL, industry text NOT NULL DEFAULT '', address text NOT NULL DEFAULT '',
    phone text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '',
    timezone text NOT NULL DEFAULT 'Asia/Jakarta', locale text NOT NULL DEFAULT 'id-ID',
    currency text NOT NULL DEFAULT 'IDR', created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), email text NOT NULL UNIQUE,
    password_hash text NOT NULL, mfa_secret_enc text NOT NULL DEFAULT '',
    mfa_enabled boolean NOT NULL DEFAULT false, last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    key text NOT NULL, name text NOT NULL, description text NOT NULL DEFAULT '', is_system boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(organization_id,key)
);
CREATE TABLE permissions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), key text NOT NULL UNIQUE, label text NOT NULL,
    description text NOT NULL DEFAULT ''
);
CREATE TABLE role_permissions (
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY(role_id,permission_id)
);
CREATE TABLE departments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name text NOT NULL, parent_department_id uuid REFERENCES departments(id) ON DELETE SET NULL,
    head_employee_id uuid, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(organization_id,name)
);
CREATE TABLE positions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    department_id uuid NOT NULL REFERENCES departments(id) ON DELETE RESTRICT, title text NOT NULL, level text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE employees (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    employee_code text NOT NULL, full_name text NOT NULL, email text NOT NULL, phone text NOT NULL DEFAULT '',
    hire_date date NOT NULL, end_date date, status text NOT NULL CHECK(status IN ('active','on_leave','inactive','terminated')),
    department_id uuid NOT NULL REFERENCES departments(id) ON DELETE RESTRICT,
    position_id uuid NOT NULL REFERENCES positions(id) ON DELETE RESTRICT,
    manager_id uuid REFERENCES employees(id) ON DELETE SET NULL, address text NOT NULL DEFAULT '', education text NOT NULL DEFAULT '',
    birth_date date, gender text NOT NULL DEFAULT '', emergency_name text NOT NULL DEFAULT '', emergency_phone text NOT NULL DEFAULT '',
    nik_encrypted text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz, UNIQUE(organization_id,employee_code)
);
ALTER TABLE departments ADD CONSTRAINT departments_head_employee_fk FOREIGN KEY(head_employee_id) REFERENCES employees(id) ON DELETE SET NULL;
CREATE INDEX employees_org_search_idx ON employees(organization_id,lower(full_name));
CREATE TABLE memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, employee_id uuid REFERENCES employees(id) ON DELETE SET NULL,
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE RESTRICT, created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(user_id,organization_id)
);
CREATE TABLE refresh_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    membership_id uuid NOT NULL REFERENCES memberships(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE, expires_at timestamptz NOT NULL, revoked_at timestamptz,
    user_agent text NOT NULL DEFAULT '', ip_address text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE mfa_recovery_codes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash text NOT NULL, used_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE compensations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE, base_salary bigint NOT NULL CHECK(base_salary >= 0),
    tax_status text NOT NULL DEFAULT 'TK/0', bpjs_health_enabled boolean NOT NULL DEFAULT true,
    bpjs_employment_enabled boolean NOT NULL DEFAULT true, bank_name text NOT NULL DEFAULT '', bank_account_enc text NOT NULL DEFAULT '',
    effective_from date NOT NULL, effective_to date, created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(employee_id)
);
CREATE TABLE holidays (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    date date NOT NULL, name text NOT NULL, UNIQUE(organization_id,date)
);
CREATE TABLE attendances (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE, date date NOT NULL, clock_in timestamptz, clock_out timestamptz,
    total_minutes integer NOT NULL DEFAULT 0 CHECK(total_minutes >= 0), overtime_minutes integer NOT NULL DEFAULT 0 CHECK(overtime_minutes >= 0),
    status text NOT NULL CHECK(status IN ('present','late','on_leave','anomaly','absent')),
    source text NOT NULL CHECK(source IN ('self','manual','rollup')), notes text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(employee_id,date)
);
CREATE TABLE shift_assignments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE, date date NOT NULL,
    shift_type text NOT NULL CHECK(shift_type IN ('morning','afternoon','night','off')),
    start_time text NOT NULL DEFAULT '', end_time text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(employee_id,date)
);
CREATE TABLE leave_types (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name text NOT NULL, annual_quota integer NOT NULL CHECK(annual_quota >= 0), color text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(organization_id,name)
);
CREATE TABLE leave_balances (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE, leave_type_id uuid NOT NULL REFERENCES leave_types(id) ON DELETE CASCADE,
    year integer NOT NULL, quota integer NOT NULL CHECK(quota >= 0), taken integer NOT NULL DEFAULT 0 CHECK(taken >= 0),
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(employee_id,leave_type_id,year)
);
CREATE TABLE leave_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE, leave_type_id uuid NOT NULL REFERENCES leave_types(id) ON DELETE RESTRICT,
    start_date date NOT NULL, end_date date NOT NULL, duration_days integer NOT NULL CHECK(duration_days > 0), reason text NOT NULL,
    status text NOT NULL CHECK(status IN ('pending','approved','rejected','cancelled')),
    approver_id uuid REFERENCES employees(id) ON DELETE SET NULL, decision_note text NOT NULL DEFAULT '', decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), CHECK(end_date >= start_date)
);
CREATE TABLE payroll_policies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name text NOT NULL, effective_from date NOT NULL, effective_to date, config jsonb NOT NULL,
    certified boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE payroll_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    policy_id uuid NOT NULL REFERENCES payroll_policies(id) ON DELETE RESTRICT, period_start date NOT NULL, period_end date NOT NULL,
    status text NOT NULL CHECK(status IN ('draft','queued','processing','completed','failed')),
    total_gross bigint NOT NULL DEFAULT 0, total_deductions bigint NOT NULL DEFAULT 0, total_net bigint NOT NULL DEFAULT 0,
    failure_message text NOT NULL DEFAULT '', processed_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(), CHECK(period_end >= period_start)
);
CREATE TABLE payslips (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    payroll_run_id uuid NOT NULL REFERENCES payroll_runs(id) ON DELETE CASCADE, employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE RESTRICT,
    gross_pay bigint NOT NULL DEFAULT 0, bpjs_deduction bigint NOT NULL DEFAULT 0, p_ph21_deduction bigint NOT NULL DEFAULT 0,
    other_deduction bigint NOT NULL DEFAULT 0, net_pay bigint NOT NULL DEFAULT 0, breakdown jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(payroll_run_id,employee_id)
);
CREATE TABLE job_postings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    department_id uuid NOT NULL REFERENCES departments(id) ON DELETE RESTRICT, title text NOT NULL,
    status text NOT NULL CHECK(status IN ('draft','open','closed')), description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE candidates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    job_posting_id uuid NOT NULL REFERENCES job_postings(id) ON DELETE RESTRICT, full_name text NOT NULL, email text NOT NULL,
    source text NOT NULL DEFAULT '', rating integer NOT NULL DEFAULT 0 CHECK(rating BETWEEN 0 AND 5),
    stage text NOT NULL CHECK(stage IN ('inbox','screening','interview','offer','hired','rejected')),
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE candidate_stage_histories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    candidate_id uuid NOT NULL REFERENCES candidates(id) ON DELETE CASCADE, from_stage text NOT NULL DEFAULT '', to_stage text NOT NULL,
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE performance_cycles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name text NOT NULL, start_date date NOT NULL, end_date date NOT NULL, status text NOT NULL CHECK(status IN ('draft','active','closed')),
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), CHECK(end_date >= start_date)
);
CREATE TABLE goals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE, cycle_id uuid NOT NULL REFERENCES performance_cycles(id) ON DELETE CASCADE,
    title text NOT NULL, category text NOT NULL DEFAULT '', progress integer NOT NULL CHECK(progress BETWEEN 0 AND 100),
    weight integer NOT NULL CHECK(weight BETWEEN 0 AND 100), created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE reviews (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cycle_id uuid NOT NULL REFERENCES performance_cycles(id) ON DELETE CASCADE, employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    reviewer_id uuid NOT NULL REFERENCES employees(id) ON DELETE RESTRICT, relation text NOT NULL DEFAULT '',
    rating integer NOT NULL CHECK(rating BETWEEN 1 AND 5), notes text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE integrations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK(provider IN ('slack','google','entra')), enabled boolean NOT NULL DEFAULT false,
    config_enc text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(organization_id,provider)
);
CREATE TABLE notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, title text NOT NULL, message text NOT NULL,
    kind text NOT NULL DEFAULT 'info', payload jsonb NOT NULL DEFAULT '{}'::jsonb, read_at timestamptz, dismissed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    title text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE conversation_members (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(conversation_id,user_id)
);
CREATE TABLE messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, sender_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    body text NOT NULL CHECK(length(body) BETWEEN 1 AND 4000), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE faqs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    category text NOT NULL, question text NOT NULL, answer text NOT NULL, sort_order integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE support_tickets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    requester_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT, subject text NOT NULL, category text NOT NULL,
    message text NOT NULL, status text NOT NULL CHECK(status IN ('open','in_progress','resolved','closed')),
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE attention_dismissals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, attention_key text NOT NULL,
    dismissed_at timestamptz NOT NULL DEFAULT now(), UNIQUE(user_id,attention_key)
);
CREATE TABLE audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL, request_id text NOT NULL DEFAULT '', action text NOT NULL,
    entity_type text NOT NULL, entity_id uuid NOT NULL, before jsonb, after jsonb, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    topic text NOT NULL, payload jsonb NOT NULL, available_at timestamptz NOT NULL DEFAULT now(), published_at timestamptz,
    attempts integer NOT NULL DEFAULT 0, last_error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_pending_idx ON outbox_events(available_at) WHERE published_at IS NULL;
CREATE TABLE idempotency_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    scope text NOT NULL, key text NOT NULL, resource_id uuid, response jsonb NOT NULL DEFAULT '{}'::jsonb,
    expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(organization_id,scope,key)
);

DO $$
DECLARE table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'roles','departments','positions','employees','memberships','refresh_sessions','compensations','holidays',
    'attendances','shift_assignments','leave_types','leave_balances','leave_requests','payroll_policies','payroll_runs',
    'payslips','job_postings','candidates','candidate_stage_histories','performance_cycles','goals','reviews','integrations',
    'notifications','conversations','conversation_members','messages','faqs','support_tickets','attention_dismissals',
    'audit_logs','outbox_events','idempotency_keys'
  ] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I USING (organization_id = nullif(current_setting(''app.organization_id'', true), '''')::uuid) WITH CHECK (organization_id = nullif(current_setting(''app.organization_id'', true), '''')::uuid)',
      table_name
    );
  END LOOP;
END $$;

CREATE OR REPLACE FUNCTION reject_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit logs are append-only';
END $$;
CREATE TRIGGER audit_logs_append_only BEFORE UPDATE OR DELETE ON audit_logs
FOR EACH ROW EXECUTE FUNCTION reject_audit_mutation();

