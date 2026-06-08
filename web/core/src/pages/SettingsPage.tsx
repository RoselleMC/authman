import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as QRCode from "qrcode";
import { Navigate, useNavigate, useParams } from "react-router-dom";
import {
  Alert,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  ConfigGrid,
  ConfigRow,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  Icon,
  Input,
  LoadingState,
  PageHeader,
  PageShell,
  PasswordInput,
  PlaceholderCard,
  PlaceholderGrid,
  Select,
  SettingsStack,
  Tabs,
  coerceAdminUser,
  coerceSystemSummary,
  cx,
  formatAbsTime,
  useI18n,
  useToast,
  type DataColumn,
  type SafeAdminUser,
} from "@authman/shared";
import {
  createAdminRole,
  createAdminUser,
  deleteAdminPasskey,
  deleteAdminUserPasskey,
  deleteAdminRole,
  disableAdminUserTOTP,
  disableAdminTOTP,
  fetchAdminAccount,
  fetchAdminPermissions,
  fetchAdminRoles,
  fetchAdminUsers,
  fetchSystemSummary,
  registerAdminPasskey,
  startAdminTOTP,
  confirmAdminTOTP,
  updateAdminAccountProfile,
  updateAdminAccountPreferences,
  updateAdminUser,
  updateAdminRole,
  type AdminAccountSecurity,
  type AdminPermission,
  type AdminRole,
} from "../api/admin";
import { useSession } from "../auth/SessionContext";

type SettingsSection = "account" | "admins" | "roles" | "system" | "security";

const SECTIONS: SettingsSection[] = ["account", "admins", "roles", "system", "security"];

function roleTone(role: string): "info" | "success" | "neutral" {
  if (role === "owner") return "info";
  if (role === "admin") return "success";
  return "neutral";
}

function roleLabel(role: Pick<AdminRole, "id" | "name" | "alias">) {
  return role.alias || role.name || role.id;
}

export function SettingsPage() {
  const { t } = useI18n();
  const { section } = useParams();
  const navigate = useNavigate();
  const current = (section ?? "admins") as SettingsSection;

  if (!SECTIONS.includes(current)) {
    return <Navigate to="/settings/account" replace />;
  }

  return (
    <PageShell>
      <PageHeader title={t("admin.settings.heading")} desc={t("admin.settings.desc")} />
      <Tabs
        value={current}
        onChange={(next) => navigate(`/settings/${next}`)}
        tabs={[
          { value: "account", label: t("admin.settings.account"), icon: "user" },
          { value: "admins", label: t("admin.settings.admins"), icon: "users" },
          { value: "roles", label: t("admin.settings.roles"), icon: "shield" },
          { value: "system", label: t("admin.settings.system"), icon: "database" },
          { value: "security", label: t("admin.settings.security"), icon: "key" },
        ]}
      />
      <div className="tab-panel">
        {current === "account" ? <AccountPanel /> : null}
        {current === "admins" ? <AdminsPanel /> : null}
        {current === "roles" ? <RolesPanel /> : null}
        {current === "system" ? <SystemPanel /> : null}
        {current === "security" ? <SecurityPanel /> : null}
      </div>
    </PageShell>
  );
}

function AccountPanel() {
  const { t } = useI18n();
  const toast = useToast();
  const qc = useQueryClient();
  const { refresh } = useSession();
  const accountQ = useQuery({ queryKey: ["admin.account"], queryFn: fetchAdminAccount });
  const account = accountQ.data;
  const security = account?.security;
  const avatarInputRef = useRef<HTMLInputElement | null>(null);
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [avatarURL, setAvatarURL] = useState("");
  const [securityDialog, setSecurityDialog] = useState<"totp" | "passkeys" | null>(null);
  const [totpSetup, setTOTPSetup] = useState<{ secret: string; otpauth_url: string } | null>(null);
  const [totpCode, setTOTPCode] = useState("");
  const [passkeyName, setPasskeyName] = useState("");
  const [deletePasskeyID, setDeletePasskeyID] = useState<string | null>(null);

  useEffect(() => {
    if (!account) return;
    setUsername(account.user.username);
    setEmail(account.user.email ?? "");
    setAvatarURL(account.user.avatar_url ?? "");
  }, [account?.user.id, account?.user.username, account?.user.email, account?.user.avatar_url]);

  const saveProfile = useMutation({
    mutationFn: (input: Parameters<typeof updateAdminAccountProfile>[0]) => updateAdminAccountProfile(input),
    onSuccess: async () => {
      toast.push({ tone: "success", title: t("admin.account.profile.saved") });
      await qc.invalidateQueries({ queryKey: ["admin.account"] });
      await qc.invalidateQueries({ queryKey: ["admin.users"] });
      await refresh();
    },
  });
  const savePrefs = useMutation({
    mutationFn: (input: Parameters<typeof updateAdminAccountPreferences>[0]) => updateAdminAccountPreferences(input),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.account.saved") });
      void qc.invalidateQueries({ queryKey: ["admin.account"] });
    },
  });
  const startTOTP = useMutation({
    mutationFn: startAdminTOTP,
    onSuccess: (setup) => {
      setTOTPSetup(setup);
      setTOTPCode("");
    },
  });
  const confirmTOTP = useMutation({
    mutationFn: () => confirmAdminTOTP(totpCode),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.account.totp.enabled") });
      setTOTPSetup(null);
      setTOTPCode("");
      setSecurityDialog(null);
      void qc.invalidateQueries({ queryKey: ["admin.account"] });
    },
  });
  const disableTOTP = useMutation({
    mutationFn: disableAdminTOTP,
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.account.totp.disabled") });
      setTOTPSetup(null);
      setTOTPCode("");
      void qc.invalidateQueries({ queryKey: ["admin.account"] });
    },
  });
  const addPasskey = useMutation({
    mutationFn: () => registerAdminPasskey(passkeyName.trim() || "Passkey"),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.account.passkey.added") });
      setPasskeyName("");
      void qc.invalidateQueries({ queryKey: ["admin.account"] });
    },
  });
  const removePasskey = useMutation({
    mutationFn: (id: string) => deleteAdminPasskey(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.account.passkey.deleted") });
      setDeletePasskeyID(null);
      void qc.invalidateQueries({ queryKey: ["admin.account"] });
    },
  });

  if (accountQ.error) return <ErrorState error={accountQ.error} onRetry={() => accountQ.refetch()} />;
  if (accountQ.isLoading || !account || !security) return <LoadingState />;
  const activeSecurity = security;

  function preferenceInput(): Parameters<typeof updateAdminAccountPreferences>[0] {
    return {
      mfa_requirement: activeSecurity.mfa_requirement,
      preferred_locale: activeSecurity.preferred_locale,
      preferred_theme: activeSecurity.preferred_theme,
    };
  }

  const passkeyUnavailable = !account.webauthn.enabled || !window.isSecureContext || !("PublicKeyCredential" in window);
  const profileDirty =
    username.trim() !== account.user.username ||
    email.trim() !== (account.user.email ?? "") ||
    avatarURL.trim() !== (account.user.avatar_url ?? "");
  const roleDisplay = account.user.role_alias ? `${account.user.role_alias} (${account.user.role})` : account.user.role;

  function handleAvatarFile(file: File | undefined) {
    if (!file) return;
    if (file.size > 256 * 1024) {
      toast.push({ tone: "warning", title: t("admin.account.avatar.tooLarge") });
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") setAvatarURL(reader.result);
    };
    reader.onerror = () => toast.push({ tone: "danger", title: t("admin.account.avatar.readFailed") });
    reader.readAsDataURL(file);
  }

  function openTOTPDialog() {
    setSecurityDialog("totp");
    if (!activeSecurity.totp_enabled && !totpSetup && !startTOTP.isPending) {
      startTOTP.mutate();
    }
  }

  return (
    <SettingsStack>
      <Card title={t("admin.account.profile")}>
        <div className="account-profile-form" data-testid="admin-account-profile">
          <div className="account-profile-form__avatar">
            <span className={cx("account-menu__avatar account-menu__avatar--lg", avatarURL.trim() && "has-image")} data-testid="account-avatar-preview">
              {avatarURL.trim() ? <img src={avatarURL.trim()} alt="" aria-hidden="true" /> : username.trim()[0]?.toUpperCase() ?? "?"}
            </span>
            <div className="account-profile-form__avatar-actions">
              <input
                ref={avatarInputRef}
                className="account-avatar-file-input"
                type="file"
                accept="image/png,image/jpeg,image/webp,image/gif,image/svg+xml"
                onChange={(event) => handleAvatarFile(event.target.files?.[0])}
                data-testid="account-avatar-file"
              />
              <Button variant="secondary" size="sm" icon="user" onClick={() => avatarInputRef.current?.click()} data-testid="account-avatar-upload">
                {t("admin.account.avatar.upload")}
              </Button>
              <Button variant="ghost" size="sm" disabled={!avatarURL.trim()} onClick={() => setAvatarURL("")} data-testid="account-avatar-remove">
                {t("admin.account.avatar.remove")}
              </Button>
            </div>
            <p>{t("admin.account.avatar.hint")}</p>
          </div>
          <div className="account-profile-form__fields">
            <div className="account-profile-form__field">
              <Field label={t("common.username")}>
                <Input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" data-testid="account-username" />
              </Field>
            </div>
            <div className="account-profile-form__field account-profile-form__field--wide">
              <Field label={t("common.email")}>
                <Input value={email} onChange={(e) => setEmail(e.target.value)} type="email" autoComplete="email" data-testid="account-email" />
              </Field>
            </div>
            <div className="account-profile-form__field account-profile-form__field--wide">
              <Field label={t("admin.settings.col.role")} hint={t("admin.account.role.hint")}>
                <Input value={roleDisplay} readOnly data-testid="account-role" />
              </Field>
            </div>
            <div className="account-profile-form__actions">
              <Button
                variant="primary"
                icon="check"
                loading={saveProfile.isPending}
                disabled={!profileDirty || username.trim() === ""}
                onClick={() => saveProfile.mutate({ username: username.trim(), email: email.trim() || undefined, avatar_url: avatarURL.trim() || undefined })}
                data-testid="account-profile-save"
              >
                {t("common.save")}
              </Button>
            </div>
          </div>
        </div>
      </Card>

      <Card title={t("admin.account.securityMethods")}>
        <p className="section-copy">{t("admin.account.securityMethods.desc")}</p>
        <div className="settings-form-grid settings-form-grid--single">
          <Field label={t("admin.account.mfaPolicy")} hint={t("admin.account.mfaPolicy.hint")}>
            <Select
              value={security.mfa_requirement}
              onChange={(next) => savePrefs.mutate({ ...preferenceInput(), mfa_requirement: next as AdminAccountSecurity["mfa_requirement"] })}
              options={[
                { value: "new_device", label: t("admin.account.mfaPolicy.newDevice") },
                { value: "always", label: t("admin.account.mfaPolicy.always") },
              ]}
              testId="account-mfa-policy"
            />
          </Field>
        </div>
        <div className="security-method-list">
          <div className="security-method-row">
            <span className="security-method-icon"><Icon name="key" size={18} /></span>
            <div className="security-method-copy">
              <strong>{t("admin.account.totp")}</strong>
              <span>{t("admin.account.totp.desc")}</span>
            </div>
            <Badge tone={security.totp_enabled ? "success" : "neutral"} dot>
              {security.totp_enabled ? t("status.enabled") : t("status.disabled")}
            </Badge>
            <Button variant="secondary" size="sm" icon="settings" onClick={openTOTPDialog} data-testid="totp-manage">
              {t("common.manage")}
            </Button>
          </div>
          <div className="security-method-row">
            <span className="security-method-icon"><Icon name="fingerprint" size={18} /></span>
            <div className="security-method-copy">
              <strong>{t("admin.account.passkeys")}</strong>
              <span>{t("admin.account.passkeys.desc")}</span>
            </div>
            <Badge tone={security.passkeys.length > 0 ? "success" : "neutral"} dot>
              {security.passkeys.length > 0 ? t("admin.account.passkeys.count").replace("{count}", String(security.passkeys.length)) : t("status.disabled")}
            </Badge>
            <Button variant="secondary" size="sm" icon="settings" onClick={() => setSecurityDialog("passkeys")} data-testid="passkey-manage">
              {t("common.manage")}
            </Button>
          </div>
        </div>
      </Card>
      <Dialog
        open={securityDialog === "totp"}
        onClose={() => !confirmTOTP.isPending && !disableTOTP.isPending && setSecurityDialog(null)}
        icon="key"
        iconTone="primary"
        title={t("admin.account.totp")}
        desc={t("admin.account.totp.setup.desc")}
        testId="totp-dialog"
        footer={
          security.totp_enabled ? (
            <>
              <Button variant="ghost" onClick={() => setSecurityDialog(null)} disabled={disableTOTP.isPending}>
                {t("common.close")}
              </Button>
              <Button variant="danger-soft" icon="trash" loading={disableTOTP.isPending} onClick={() => disableTOTP.mutate()} data-testid="totp-disable">
                {t("common.disable")}
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" onClick={() => setSecurityDialog(null)} disabled={confirmTOTP.isPending}>
                {t("common.cancel")}
              </Button>
              <Button variant="primary" icon="check" loading={confirmTOTP.isPending} disabled={!totpSetup || totpCode.trim().length < 6} onClick={() => confirmTOTP.mutate()} data-testid="totp-confirm">
                {t("admin.account.totp.confirm")}
              </Button>
            </>
          )
        }
      >
        {security.totp_enabled ? (
          <div className="security-status-row">
            <Badge tone="success" dot>{t("status.enabled")}</Badge>
            <span>{t("admin.account.totp.enabled")}</span>
          </div>
        ) : (
          <div className="totp-dialog-grid" data-testid="totp-setup">
            {totpSetup ? <QRCodeImage value={totpSetup.otpauth_url} /> : <div className="totp-qr is-loading" data-testid="totp-qr" />}
            <div className="totp-dialog-fields">
              <Field label={t("admin.account.totp.secret")}>
                <code className="mono totp-secret">{totpSetup?.secret ?? "..."}</code>
              </Field>
              {totpSetup ? <a className="inline-link" href={totpSetup.otpauth_url}>{t("admin.account.totp.openApp")}</a> : null}
              <Field label={t("login.mfa.totp")}>
                <Input value={totpCode} inputMode="numeric" autoComplete="one-time-code" onChange={(e) => setTOTPCode(e.target.value)} data-testid="totp-confirm-code" />
              </Field>
            </div>
          </div>
        )}
      </Dialog>
      <Dialog
        open={securityDialog === "passkeys"}
        onClose={() => !addPasskey.isPending && setSecurityDialog(null)}
        icon="fingerprint"
        iconTone="primary"
        title={t("admin.account.passkeys")}
        desc={t("admin.account.passkeys.desc")}
        testId="passkey-dialog"
        footer={
          <Button variant="ghost" onClick={() => setSecurityDialog(null)} disabled={addPasskey.isPending}>
            {t("common.close")}
          </Button>
        }
      >
        {passkeyUnavailable ? <Alert tone="warning">{t("admin.account.passkeys.secureContext")}</Alert> : null}
        <div className="passkey-add-row">
          <Input value={passkeyName} onChange={(e) => setPasskeyName(e.target.value)} placeholder={t("admin.account.passkeys.name")} data-testid="passkey-name" />
          <Button variant="secondary" icon="fingerprint" disabled={passkeyUnavailable} loading={addPasskey.isPending} onClick={() => addPasskey.mutate()} data-testid="passkey-add">
            {t("admin.account.passkeys.add")}
          </Button>
        </div>
        <div className="passkey-list">
          {security.passkeys.length === 0 ? (
            <EmptyState icon="fingerprint" title={t("admin.account.passkeys.empty")} />
          ) : (
            security.passkeys.map((passkey) => (
              <div className="passkey-row" key={passkey.id}>
                <div>
                  <strong>{passkey.name}</strong>
                  <span>{t("admin.account.passkeys.created")} {formatAbsTime(passkey.created_at)}</span>
                </div>
                <Button variant="danger-soft" size="sm" icon="trash" onClick={() => setDeletePasskeyID(passkey.id)} data-testid={`passkey-delete-${passkey.id}`}>
                  {t("common.delete")}
                </Button>
              </div>
            ))
          )}
        </div>
      </Dialog>
      <ConfirmDialog
        open={deletePasskeyID !== null}
        destructive
        title={t("admin.account.passkeys.delete")}
        body={t("admin.account.passkeys.delete.desc")}
        confirmLabel={t("common.delete")}
        loading={removePasskey.isPending}
        onCancel={() => setDeletePasskeyID(null)}
        onConfirm={() => deletePasskeyID && removePasskey.mutate(deletePasskeyID)}
        testId="passkey-delete-dialog"
      />
    </SettingsStack>
  );
}

function QRCodeImage({ value }: { value: string }) {
  const [src, setSrc] = useState("");

  useEffect(() => {
    let cancelled = false;
    QRCode.toDataURL(value, {
      margin: 1,
      width: 184,
      color: { dark: "#11231a", light: "#ffffff" },
    }).then((next) => {
      if (!cancelled) setSrc(next);
    }).catch(() => {
      if (!cancelled) setSrc("");
    });
    return () => {
      cancelled = true;
    };
  }, [value]);

  return src ? <img className="totp-qr" src={src} alt="" data-testid="totp-qr" /> : <div className="totp-qr is-loading" data-testid="totp-qr" />;
}

function AdminsPanel() {
  const { t } = useI18n();
  const { hasPermission, user: currentUser } = useSession();
  const toast = useToast();
  const qc = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<SafeAdminUser | null>(null);
  const [totpResetUser, setTOTPResetUser] = useState<SafeAdminUser | null>(null);
  const [passkeyDelete, setPasskeyDelete] = useState<{ user: SafeAdminUser; passkeyID: string; passkeyName: string } | null>(null);
  const usersQ = useQuery({ queryKey: ["admin.users"], queryFn: fetchAdminUsers });
  const rolesQ = useQuery({ queryKey: ["admin.roles"], queryFn: fetchAdminRoles });
  const adminUsers: SafeAdminUser[] = (usersQ.data ?? []).map(coerceAdminUser);
  const roles = rolesQ.data ?? [];
  const rolesByID = useMemo(() => new Map(roles.map((role) => [role.id, role])), [roles]);
  const canEditAdmins = hasPermission("admin.users.write");
  const canManageAdminSecurity = hasPermission("admin.users.security.write");
  const currentIsOwner = currentUser?.role === "owner" || currentUser?.permissions.includes("*");
  const createMut = useMutation({
    mutationFn: createAdminUser,
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.settings.admin.created") });
      setDialogOpen(false);
      void qc.invalidateQueries({ queryKey: ["admin.users"] });
    },
  });
  const updateMut = useMutation({
    mutationFn: (input: { id: string; username: string; email?: string; role: string; status: "active" | "disabled" }) => {
      const { id, ...body } = input;
      return updateAdminUser(id, body);
    },
    onSuccess: async () => {
      toast.push({ tone: "success", title: t("admin.settings.admin.saved") });
      setEditingUser(null);
      await qc.invalidateQueries({ queryKey: ["admin.users"] });
      await qc.invalidateQueries({ queryKey: ["admin.session"] });
    },
  });
  const disableTargetTOTP = useMutation({
    mutationFn: (id: string) => disableAdminUserTOTP(id),
    onSuccess: async (security) => {
      toast.push({ tone: "success", title: t("admin.settings.admin.totp.disabled") });
      setEditingUser((current) => current ? { ...current, security } : current);
      setTOTPResetUser(null);
      await qc.invalidateQueries({ queryKey: ["admin.users"] });
    },
  });
  const deleteTargetPasskey = useMutation({
    mutationFn: (input: { userID: string; passkeyID: string }) => deleteAdminUserPasskey(input.userID, input.passkeyID),
    onSuccess: async (security) => {
      toast.push({ tone: "success", title: t("admin.account.passkey.deleted") });
      setEditingUser((current) => current ? { ...current, security } : current);
      setPasskeyDelete(null);
      await qc.invalidateQueries({ queryKey: ["admin.users"] });
    },
  });
  const columns: DataColumn<SafeAdminUser>[] = [
    {
      key: "name",
      header: t("admin.settings.col.admin"),
      render: (u) => (
        <div className="admin-user">
          <span className="acct-avatar" style={{ width: 30, height: 30 }}>
            {(u.display_name || u.username || u.email)[0]?.toUpperCase() ?? "?"}
          </span>
          <div>
            <div className="admin-user-name">{u.display_name || u.username || u.email || "—"}</div>
            <code className="mono admin-user-email">{u.email || "—"}</code>
          </div>
        </div>
      ),
    },
    {
      key: "role",
      header: t("admin.settings.col.role"),
      render: (u) => {
        const role = rolesByID.get(u.role);
        const label = u.role_alias || (role ? roleLabel(role) : t(`admin.settings.role.${u.role}`, u.role));
        return (
          <span className="role-inline">
            <span>{label}</span>
            <Badge tone={roleTone(u.role)}>{u.role}</Badge>
          </span>
        );
      },
    },
    {
      key: "status",
      header: t("admin.settings.col.status"),
      render: (u) =>
        u.status === "active" ? (
          <Badge tone="success" dot>
            {t("status.active")}
          </Badge>
        ) : (
          <Badge tone="neutral" dot>
            {t("status.disabled")}
          </Badge>
        ),
    },
    {
      key: "security",
      header: t("admin.settings.col.security"),
      render: (u) => (
        <div className="admin-security-summary">
          <Badge tone={u.security.totp_enabled ? "success" : "neutral"} dot>
            {u.security.totp_enabled ? t("admin.account.totp") : t("admin.settings.admin.totp.none")}
          </Badge>
          <span>{t("admin.account.passkeys.count").replace("{count}", String(u.security.passkeys.length))}</span>
        </div>
      ),
    },
    { key: "created", header: t("admin.settings.col.created"), render: (u) => (u.created_at ? formatAbsTime(u.created_at) : "—") },
    {
      key: "actions",
      header: "",
      align: "right",
      render: (u) => {
        const ownerProtected = u.role === "owner" && !currentIsOwner;
        const disabled = ownerProtected || (!canEditAdmins && !canManageAdminSecurity);
        return (
          <div className="row-actions">
            <Button
              variant="secondary"
              size="sm"
              icon="settings"
              disabled={disabled}
              title={ownerProtected ? t("admin.settings.admin.ownerProtected") : disabled ? t("admin.settings.readOnly") : undefined}
              onClick={() => setEditingUser(u)}
              data-testid={`edit-admin-${u.id}`}
            >
              {t("common.edit")}
            </Button>
          </div>
        );
      },
    },
  ];

  return (
    <SettingsStack>
      <Card
        title={t("admin.settings.admins")}
        noBody
        actions={
          <Button
            variant="secondary"
            size="sm"
            icon="plus"
            disabled={!canEditAdmins}
            title={canEditAdmins ? undefined : t("admin.settings.readOnly")}
            onClick={() => setDialogOpen(true)}
            data-testid="add-admin"
          >
            {t("admin.settings.addAdmin")}
          </Button>
        }
      >
        {usersQ.error ? (
          <ErrorState error={usersQ.error} onRetry={() => usersQ.refetch()} />
        ) : (
          <DataTable
            loading={usersQ.isLoading}
            rows={adminUsers}
            columns={columns}
            rowKey={(r) => r.id}
            empty={<EmptyState icon="users" title={t("admin.settings.emptyAdmins")} />}
            testId="admin-users-table"
          />
        )}
        <div className="card-foot-note">
          <Icon name="info" size={13} /> {t("admin.settings.footnote")}
        </div>
      </Card>
      <AdminUserDialog
        open={dialogOpen}
        roles={roles}
        loadingRoles={rolesQ.isLoading}
        pending={createMut.isPending}
        error={createMut.error}
        onClose={() => !createMut.isPending && setDialogOpen(false)}
        onSubmit={(input) => createMut.mutate(input)}
      />
      <AdminUserDialog
        open={editingUser !== null}
        mode="edit"
        user={editingUser}
        roles={roles}
        loadingRoles={rolesQ.isLoading}
        pending={updateMut.isPending}
        error={updateMut.error}
        canEdit={canEditAdmins && (!!currentIsOwner || editingUser?.role !== "owner")}
        canManageSecurity={canManageAdminSecurity && (!!currentIsOwner || editingUser?.role !== "owner")}
        ownerProtected={editingUser?.role === "owner" && !currentIsOwner}
        onClose={() => !updateMut.isPending && setEditingUser(null)}
        onSubmit={(input) =>
          editingUser &&
          updateMut.mutate({
            id: editingUser.id,
            username: input.username,
            email: input.email,
            role: input.role,
            status: input.status,
          })
        }
        onDisableTOTP={(user) => setTOTPResetUser(user)}
        onDeletePasskey={(user, passkey) => setPasskeyDelete({ user, passkeyID: passkey.id, passkeyName: passkey.name })}
      />
      <ConfirmDialog
        open={totpResetUser !== null}
        destructive
        title={t("admin.settings.admin.totp.disable")}
        body={t("admin.settings.admin.totp.disable.desc").replace("{username}", totpResetUser?.username ?? "")}
        confirmLabel={t("common.disable")}
        loading={disableTargetTOTP.isPending}
        onCancel={() => setTOTPResetUser(null)}
        onConfirm={() => totpResetUser && disableTargetTOTP.mutate(totpResetUser.id)}
        testId="admin-totp-disable-dialog"
      />
      <ConfirmDialog
        open={passkeyDelete !== null}
        destructive
        title={t("admin.settings.admin.passkey.delete")}
        body={t("admin.settings.admin.passkey.delete.desc")
          .replace("{passkey}", passkeyDelete?.passkeyName ?? "")
          .replace("{username}", passkeyDelete?.user.username ?? "")}
        confirmLabel={t("common.delete")}
        loading={deleteTargetPasskey.isPending}
        onCancel={() => setPasskeyDelete(null)}
        onConfirm={() => passkeyDelete && deleteTargetPasskey.mutate({ userID: passkeyDelete.user.id, passkeyID: passkeyDelete.passkeyID })}
        testId="admin-passkey-delete-dialog"
      />
    </SettingsStack>
  );
}

function AdminUserDialog({
  open,
  mode = "create",
  user,
  roles,
  loadingRoles,
  pending,
  error,
  canEdit = true,
  canManageSecurity = false,
  ownerProtected = false,
  onClose,
  onSubmit,
  onDisableTOTP,
  onDeletePasskey,
}: {
  open: boolean;
  mode?: "create" | "edit";
  user?: SafeAdminUser | null;
  roles: AdminRole[];
  loadingRoles: boolean;
  pending: boolean;
  error: unknown;
  canEdit?: boolean;
  canManageSecurity?: boolean;
  ownerProtected?: boolean;
  onClose: () => void;
  onSubmit: (input: { username: string; email?: string; password: string; role: string; status: "active" | "disabled" }) => void;
  onDisableTOTP?: (user: SafeAdminUser) => void;
  onDeletePasskey?: (user: SafeAdminUser, passkey: SafeAdminUser["security"]["passkeys"][number]) => void;
}) {
  const { t } = useI18n();
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("admin");
  const [status, setStatus] = useState<"active" | "disabled">("active");

  useEffect(() => {
    if (!open) return;
    if (mode === "edit" && user) {
      setUsername(user.username);
      setEmail(user.email);
      setPassword("");
      setRole(user.role);
      setStatus(user.status);
      return;
    }
    setUsername("");
    setEmail("");
    setPassword("");
    setRole(roles.find((r) => r.id === "admin")?.id ?? roles.find((r) => !r.system)?.id ?? roles[0]?.id ?? "admin");
    setStatus("active");
  }, [open, mode, user?.id, user?.username, user?.email, user?.role, user?.status, roles]);

  const isEdit = mode === "edit";
  const bootstrapLocked = user?.id === "bootstrap-admin";
  const profileDisabled = !canEdit;
  const roleStatusDisabled = !canEdit || bootstrapLocked;
  const title = isEdit ? t("admin.settings.admin.edit") : t("admin.settings.admin.create");
  const desc = isEdit ? t("admin.settings.admin.edit.desc") : t("admin.settings.admin.create.desc");
  const submitLabel = isEdit ? t("common.save") : t("admin.settings.addAdmin");

  return (
    <Dialog
      open={open}
      onClose={onClose}
      icon="users"
      iconTone="primary"
      title={title}
      desc={desc}
      testId="admin-user-dialog"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={pending} data-testid="admin-user-cancel">
            {t("common.cancel")}
          </Button>
          <Button
            variant="primary"
            icon={isEdit ? "check" : "plus"}
            loading={pending}
            disabled={pending || !canEdit || username.trim() === "" || (!isEdit && password === "") || loadingRoles}
            onClick={() => onSubmit({ username: username.trim(), email: email.trim() || undefined, password, role, status })}
            data-testid="admin-user-submit"
          >
            {submitLabel}
          </Button>
        </>
      }
    >
      <div className="dialog-form">
        {bootstrapLocked ? <Alert tone="warning">{t("admin.settings.admin.bootstrapLocked")}</Alert> : null}
        {ownerProtected ? <Alert tone="warning">{t("admin.settings.admin.ownerProtected")}</Alert> : null}
        <Field label={t("admin.settings.admin.username")}>
          <Input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus disabled={profileDisabled} data-testid="admin-user-username" />
        </Field>
        <Field label={t("admin.settings.admin.email")} hint={t("admin.settings.admin.email.hint")}>
          <Input value={email} onChange={(e) => setEmail(e.target.value)} type="email" disabled={profileDisabled} data-testid="admin-user-email" />
        </Field>
        {!isEdit ? (
          <Field label={t("admin.settings.admin.password")}>
            <PasswordInput value={password} onChange={(e) => setPassword(e.target.value)} data-testid="admin-user-password" />
          </Field>
        ) : null}
        <Field label={t("admin.settings.admin.role")}>
          <Select
            value={role}
            onChange={setRole}
            disabled={loadingRoles || roleStatusDisabled}
            options={roles.map((item) => ({ value: item.id, label: `${roleLabel(item)} (${item.id})` }))}
            testId="admin-user-role"
          />
        </Field>
        {isEdit ? (
          <Field label={t("admin.settings.col.status")}>
            <Select
              value={status}
              onChange={(next) => setStatus(next as "active" | "disabled")}
              disabled={roleStatusDisabled}
              options={[
                { value: "active", label: t("status.active") },
                { value: "disabled", label: t("status.disabled") },
              ]}
              testId="admin-user-status"
            />
          </Field>
        ) : null}
        {isEdit && user ? (
          <section className="admin-security-editor" data-testid="admin-security-editor">
            <div className="admin-security-editor__head">
              <div>
                <h4>{t("admin.settings.admin.security")}</h4>
                <p>{t("admin.settings.admin.security.desc")}</p>
              </div>
              {!canManageSecurity ? <Badge tone="neutral">{t("admin.settings.readOnly")}</Badge> : null}
            </div>
            <div className="admin-security-editor__row">
              <div>
                <strong>{t("admin.account.totp")}</strong>
                <span>{user.security.totp_enabled ? t("status.enabled") : t("status.disabled")}</span>
              </div>
              <Button
                variant="danger-soft"
                size="sm"
                icon="trash"
                disabled={!canManageSecurity || !user.security.totp_enabled}
                onClick={() => onDisableTOTP?.(user)}
                data-testid="admin-user-disable-totp"
              >
                {t("common.disable")}
              </Button>
            </div>
            <div className="admin-security-editor__passkeys">
              <strong>{t("admin.account.passkeys")}</strong>
              {user.security.passkeys.length === 0 ? (
                <EmptyState icon="fingerprint" title={t("admin.account.passkeys.empty")} />
              ) : (
                user.security.passkeys.map((passkey) => (
                  <div className="passkey-row" key={passkey.id}>
                    <div>
                      <strong>{passkey.name}</strong>
                      <span>{t("admin.account.passkeys.created")} {passkey.created_at ? formatAbsTime(passkey.created_at) : "—"}</span>
                    </div>
                    <Button
                      variant="danger-soft"
                      size="sm"
                      icon="trash"
                      disabled={!canManageSecurity}
                      onClick={() => onDeletePasskey?.(user, passkey)}
                      data-testid={`admin-user-passkey-delete-${passkey.id}`}
                    >
                      {t("common.delete")}
                    </Button>
                  </div>
                ))
              )}
            </div>
          </section>
        ) : null}
        {error ? <Alert tone="warning">{String((error as { message?: string }).message ?? error)}</Alert> : null}
      </div>
    </Dialog>
  );
}

function RolesPanel() {
  const { t } = useI18n();
  const { hasPermission } = useSession();
  const toast = useToast();
  const qc = useQueryClient();
  const rolesQ = useQuery({ queryKey: ["admin.roles"], queryFn: fetchAdminRoles });
  const permissionsQ = useQuery({ queryKey: ["admin.permissions"], queryFn: fetchAdminPermissions });
  const roles = rolesQ.data ?? [];
  const [selectedID, setSelectedID] = useState<string>("admin");
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteRoleID, setDeleteRoleID] = useState<string | null>(null);
  const selected = roles.find((role) => role.id === selectedID) ?? roles[0] ?? null;
  const [draft, setDraft] = useState<string[]>([]);

  useEffect(() => {
    if (selected) {
      setDraft(selected.permissions);
    }
  }, [selected?.id, selected?.permissions.join("|")]);

  const grouped = useMemo(() => groupPermissions(permissionsQ.data ?? []), [permissionsQ.data]);
  const dirty = selected ? normalizedList(draft).join("|") !== normalizedList(selected.permissions).join("|") : false;
  const canWrite = hasPermission("admin.roles.write") && !!selected && !selected.system;
  const save = useMutation({
    mutationFn: () => updateAdminRole(selected!.id, draft, selected?.alias ?? selected?.name, selected?.description),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.settings.roles.saved") });
      void qc.invalidateQueries({ queryKey: ["admin.roles"] });
      void qc.invalidateQueries({ queryKey: ["admin.session"] });
    },
  });
  const createRole = useMutation({
    mutationFn: createAdminRole,
    onSuccess: (role) => {
      toast.push({ tone: "success", title: t("admin.settings.roles.created") });
      setCreateOpen(false);
      setSelectedID(role.id);
      void qc.invalidateQueries({ queryKey: ["admin.roles"] });
    },
  });
  const deleteRole = useMutation({
    mutationFn: (id: string) => deleteAdminRole(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.settings.roles.deleted") });
      setDeleteRoleID(null);
      setSelectedID("admin");
      void qc.invalidateQueries({ queryKey: ["admin.roles"] });
    },
  });

  if (rolesQ.error || permissionsQ.error) {
    return <ErrorState error={rolesQ.error ?? permissionsQ.error} onRetry={() => { void rolesQ.refetch(); void permissionsQ.refetch(); }} />;
  }
  if (rolesQ.isLoading || permissionsQ.isLoading || !selected) {
    return <LoadingState />;
  }

  function roleHas(permission: AdminPermission) {
    return draft.includes("*") || draft.includes(permission.key) || draft.includes(`${permission.group}.*`);
  }

  function toggle(permission: AdminPermission, checked: boolean) {
    setDraft((current) => {
      const next = new Set(current.filter((p) => p !== "*"));
      if (checked) {
        next.add(permission.key);
      } else {
        next.delete(permission.key);
      }
      return normalizedList(Array.from(next));
    });
  }

  return (
    <div className="roles-layout">
      <Card
        title={t("admin.settings.roles")}
        noBody
        className="roles-sidebar-card"
        actions={
          <Button
            variant="secondary"
            size="sm"
            icon="plus"
            disabled={!hasPermission("admin.roles.write")}
            onClick={() => setCreateOpen(true)}
            data-testid="create-role"
          >
            {t("admin.settings.roles.create")}
          </Button>
        }
      >
        <div className="role-list" data-testid="role-list">
          {roles.map((role) => (
            <button
              key={role.id}
              type="button"
              className={cx("role-card", role.id === selected.id && "is-active")}
              onClick={() => setSelectedID(role.id)}
              data-testid={`role-${role.id}`}
            >
              <span className="role-card-title">
                <span className="role-card-name">{t(`admin.settings.role.${role.id}`, roleLabel(role))}</span>
                <span className="role-card-badges">
                  <Badge tone="neutral">{role.id}</Badge>
                  {role.system ? <Badge tone="neutral">{t("admin.settings.roles.system")}</Badge> : null}
                </span>
              </span>
              <span className="role-card-desc">{role.description}</span>
              <span className="role-card-count">{role.permissions.includes("*") ? t("admin.settings.roles.allPermissions") : t("admin.settings.roles.permissionCount").replace("{count}", String(role.permissions.length))}</span>
            </button>
          ))}
        </div>
      </Card>

      <Card
        title={
          <span className="role-title">
            <span>{t(`admin.settings.role.${selected.id}`, roleLabel(selected))}</span>
            <Badge tone="neutral">{selected.id}</Badge>
          </span>
        }
        actions={
          <>
            {!selected.system && hasPermission("admin.roles.write") ? (
              <Button
                variant="danger-soft"
                size="sm"
                icon="trash"
                disabled={deleteRole.isPending}
                onClick={() => setDeleteRoleID(selected.id)}
                data-testid="delete-role"
              >
                {t("common.delete")}
              </Button>
            ) : null}
            <Button
              variant="primary"
              size="sm"
              icon="check"
              disabled={!canWrite || !dirty || save.isPending}
              loading={save.isPending}
              onClick={() => save.mutate()}
              data-testid="save-role"
            >
              {t("common.save")}
            </Button>
          </>
        }
      >
        <div className="role-detail-head">
          <p>{selected.description}</p>
          {selected.system ? (
            <Alert tone="info" testId="system-role-note">{t("admin.settings.roles.ownerLocked")}</Alert>
          ) : !hasPermission("admin.roles.write") ? (
            <Alert tone="warning">{t("common.permissionDenied")}</Alert>
          ) : null}
        </div>

        <div className="permission-groups" data-testid="permission-groups">
          {grouped.map(([group, permissions]) => (
            <section className="permission-group" key={group}>
              <div className="permission-group-head">
                <h3>{t(`admin.settings.permission.group.${group}`, group)}</h3>
                <span className="mono">{permissions.filter(roleHas).length}/{permissions.length}</span>
              </div>
              <div className="permission-list">
                {permissions.map((permission) => (
                  <label className="permission-row" key={permission.key}>
                    <input
                      type="checkbox"
                      checked={roleHas(permission)}
                      disabled={!canWrite}
                      onChange={(e) => toggle(permission, e.target.checked)}
                    />
                    <span className="permission-copy">
                      <span className="permission-title">{permission.label}</span>
                      <code>{permission.key}</code>
                      <span>{permission.description}</span>
                    </span>
                  </label>
                ))}
              </div>
            </section>
          ))}
        </div>
      </Card>
      <RoleCreateDialog
        open={createOpen}
        pending={createRole.isPending}
        error={createRole.error}
        onClose={() => !createRole.isPending && setCreateOpen(false)}
        onSubmit={(input) => createRole.mutate(input)}
      />
      <ConfirmDialog
        open={deleteRoleID !== null}
        destructive
        title={t("admin.settings.roles.delete")}
        body={t("admin.settings.roles.delete.desc")}
        confirmLabel={t("common.delete")}
        loading={deleteRole.isPending}
        onCancel={() => setDeleteRoleID(null)}
        onConfirm={() => deleteRoleID && deleteRole.mutate(deleteRoleID)}
        testId="role-delete-dialog"
      />
    </div>
  );
}

function RoleCreateDialog({
  open,
  pending,
  error,
  onClose,
  onSubmit,
}: {
  open: boolean;
  pending: boolean;
  error: unknown;
  onClose: () => void;
  onSubmit: (input: { role_id: string; alias?: string; description?: string; permissions: string[] }) => void;
}) {
  const { t } = useI18n();
  const [roleID, setRoleID] = useState("");
  const [alias, setAlias] = useState("");
  const [description, setDescription] = useState("");

  useEffect(() => {
    if (!open) return;
    setRoleID("");
    setAlias("");
    setDescription("");
  }, [open]);

  return (
    <Dialog
      open={open}
      onClose={onClose}
      icon="shield"
      iconTone="primary"
      title={t("admin.settings.roles.create")}
      desc={t("admin.settings.roles.create.desc")}
      testId="role-create-dialog"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={pending} data-testid="role-create-cancel">
            {t("common.cancel")}
          </Button>
          <Button
            variant="primary"
            icon="plus"
            loading={pending}
            disabled={pending || roleID.trim() === ""}
            onClick={() => onSubmit({ role_id: roleID.trim(), alias: alias.trim() || undefined, description: description.trim() || undefined, permissions: [] })}
            data-testid="role-create-submit"
          >
            {t("admin.settings.roles.create")}
          </Button>
        </>
      }
    >
      <div className="dialog-form">
        <Field label={t("admin.settings.roles.roleId")} hint={t("admin.settings.roles.roleId.hint")}>
          <Input value={roleID} onChange={(e) => setRoleID(e.target.value)} mono autoFocus placeholder="support.readonly" data-testid="role-create-id" />
        </Field>
        <Field label={t("admin.settings.roles.alias")} hint={t("admin.settings.roles.alias.hint")}>
          <Input value={alias} onChange={(e) => setAlias(e.target.value)} placeholder={t("admin.settings.roles.alias.placeholder")} data-testid="role-create-alias" />
        </Field>
        <Field label={t("admin.settings.roles.description")}>
          <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder={t("admin.settings.roles.description.placeholder")} data-testid="role-create-description" />
        </Field>
        {error ? <Alert tone="warning">{String((error as { message?: string }).message ?? error)}</Alert> : null}
      </div>
    </Dialog>
  );
}

function SystemPanel() {
  const sysQ = useQuery({ queryKey: ["admin.system"], queryFn: fetchSystemSummary });
  const { t } = useI18n();
  return (
    <Card title={t("admin.settings.system")}>
      {sysQ.error ? (
        <ErrorState error={sysQ.error} onRetry={() => sysQ.refetch()} />
      ) : sysQ.isLoading || !sysQ.data ? (
        <LoadingState />
      ) : (
        <SystemSummaryGrid raw={sysQ.data} />
      )}
    </Card>
  );
}

function SecurityPanel() {
  const { t } = useI18n();
  return (
    <PlaceholderGrid>
      <PlaceholderCard
        icon="external"
        title={t("admin.settings.smtp")}
        desc={t("admin.settings.smtp.placeholder")}
        testId="placeholder-smtp"
      />
      <PlaceholderCard
        icon="key"
        title={t("admin.settings.2fa")}
        desc={t("admin.settings.2fa.placeholder")}
        testId="placeholder-2fa"
      />
    </PlaceholderGrid>
  );
}

function SystemSummaryGrid({ raw }: { raw: unknown }) {
  const { t } = useI18n();
  const sys = coerceSystemSummary(raw);
  const flagsLabel =
    Object.keys(sys.feature_flags).length === 0
      ? "—"
      : Object.entries(sys.feature_flags)
          .map(([k, v]) => `${k}: ${v}`)
          .join(", ");
  return (
    <ConfigGrid testId="system-summary">
      <ConfigRow k={t("admin.settings.system.version")} v={sys.version} mono />
      <ConfigRow k={t("admin.settings.system.environment")} v={sys.environment} mono />
      <ConfigRow k={t("admin.settings.system.database")} v={sys.database} ok={sys.database !== "unknown"} />
      <ConfigRow k={t("admin.settings.system.uptime")} v={sys.uptime_seconds == null ? "—" : `${Math.round(sys.uptime_seconds / 60)} ${t("common.minutesShort")}`} />
      <ConfigRow k={t("admin.settings.system.featureFlags")} v={flagsLabel} />
      {sys.extra_rows.map((row) => (
        <ConfigRow key={row.k} k={row.k} v={row.v} />
      ))}
    </ConfigGrid>
  );
}

function groupPermissions(permissions: AdminPermission[]) {
  const groups = new Map<string, AdminPermission[]>();
  for (const permission of permissions) {
    const list = groups.get(permission.group) ?? [];
    list.push(permission);
    groups.set(permission.group, list);
  }
  return Array.from(groups.entries());
}

function normalizedList(values: string[]) {
  return Array.from(new Set(values)).sort();
}
