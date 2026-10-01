DROP TRIGGER IF EXISTS audit_logs_append_only ON audit_logs;
DROP FUNCTION IF EXISTS reject_audit_mutation();
DROP TABLE IF EXISTS idempotency_keys, outbox_events, audit_logs, attention_dismissals,
  support_tickets, faqs, messages, conversation_members, conversations, notifications,
  integrations, reviews, goals, performance_cycles, candidate_stage_histories, candidates,
  job_postings, payslips, payroll_runs, payroll_policies, leave_requests, leave_balances,
  leave_types, shift_assignments, attendances, holidays, compensations, refresh_sessions,
  mfa_recovery_codes, memberships, employees, positions, departments, role_permissions,
  permissions, roles, users, organizations CASCADE;
