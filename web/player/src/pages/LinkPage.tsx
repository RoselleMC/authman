import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import {
  Alert,
  ApiError,
  Button,
  Icon,
  useI18n,
} from "@authman/shared";
import { useSession } from "../auth/SessionContext";
import { useServerContext } from "../server-context/ServerContextProvider";
import { AuthHeader } from "../components/AuthHeader";

type Phase = "processing" | "success" | "failure";
type FailCode = "missing" | "expired" | "used" | "revoked" | "not_found";

function parseTokenFromFragment(hash: string): string | null {
  if (!hash || hash.length < 2) return null;
  const stripped = hash.startsWith("#") ? hash.slice(1) : hash;
  const params = new URLSearchParams(stripped);
  return params.get("token");
}

/**
 * Tracks which router-location keys have already been processed by a LinkPage
 * instance. Module-level (not useRef) so that React StrictMode's intentional
 * double mount-unmount cycle does not re-consume the URL fragment.
 */
const PROCESSED_LOCATION_KEYS = new Set<string>();

const FAIL_INFO: Record<FailCode, { icon: string; titleKey: string; recover: "offline" | "premium" }> = {
  missing: { icon: "alert", titleKey: "link.error.missing", recover: "offline" },
  expired: { icon: "clock", titleKey: "link.error.expired", recover: "offline" },
  used: { icon: "check", titleKey: "link.error.used", recover: "offline" },
  revoked: { icon: "shield", titleKey: "link.error.revoked", recover: "premium" },
  not_found: { icon: "alert", titleKey: "link.error.not_found", recover: "offline" },
};

export function LinkPage() {
  const { t } = useI18n();
  const { loginWithLink } = useSession();
  const { slug } = useServerContext();
  const navigate = useNavigate();
  const location = useLocation();
  const [phase, setPhase] = useState<Phase>("processing");
  const [failCode, setFailCode] = useState<FailCode>("not_found");
  const [fragmentCleared, setFragmentCleared] = useState(false);
  useEffect(() => {
    if (PROCESSED_LOCATION_KEYS.has(location.key)) return;
    PROCESSED_LOCATION_KEYS.add(location.key);
    const token = parseTokenFromFragment(window.location.hash);
    if (window.location.hash) {
      const url = window.location.pathname + window.location.search;
      window.history.replaceState(null, "", url);
      setFragmentCleared(true);
    }
    if (!token) {
      setPhase("failure");
      setFailCode("missing");
      return;
    }
    (async () => {
      try {
        const me = await loginWithLink(token);
        setPhase("success");
        const targetSlug = me.server_slug ?? slug ?? null;
        const target = targetSlug ? `/server/${targetSlug}/account` : "/account";
        window.setTimeout(() => navigate(target, { replace: true }), 600);
      } catch (err) {
        setPhase("failure");
        if (err instanceof ApiError) {
          if (err.code === "portal.link_expired") setFailCode("expired");
          else if (err.code === "portal.link_used") setFailCode("used");
          else if (err.code === "portal.link_revoked") setFailCode("revoked");
          else setFailCode("not_found");
        } else {
          setFailCode("not_found");
        }
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.key]);

  return (
    <div className="pauth">
      <AuthHeader />
      <div className="pauth-body">
        <div className="pauth-card link-card">
          {phase === "processing" ? (
            <div className="link-state" data-testid="link-processing">
              <div className="link-spinner">
                <span className="spinner" style={{ width: 28, height: 28 }} />
              </div>
              <h1>{t("link.processing")}</h1>
              <p>{t("link.reading")}</p>
              {fragmentCleared ? (
                <div className="frag-note">
                  <Icon name="check" size={13} /> {t("link.tokenCleared")}
                </div>
              ) : null}
            </div>
          ) : null}

          {phase === "success" ? (
            <div className="link-state" data-testid="link-success">
              <div className="link-icon link-icon--ok">
                <Icon name="check" size={28} />
              </div>
              <h1>{t("link.signedIn.heading")}</h1>
              <p>{t("link.signedIn.body")}</p>
              <div className="frag-note">
                <Icon name="lock" size={13} /> {t("link.tokenNotStored")}
              </div>
            </div>
          ) : null}

          {phase === "failure" ? (
            <FailureState code={failCode} recoverTo={slug ? `/server/${slug}/login` : "/login"} />
          ) : null}
        </div>
      </div>
    </div>
  );
}

function FailureState({ code, recoverTo }: { code: FailCode; recoverTo: string }) {
  const { t } = useI18n();
  const info = FAIL_INFO[code];
  return (
    <div className="link-state" data-testid="link-failure">
      <div className="link-icon link-icon--bad">
        <Icon name={info.icon} size={26} />
      </div>
      <h1 data-testid="link-failure-message">{t(info.titleKey)}</h1>
      <p data-testid="link-failure-recovery">
        {info.recover === "offline" ? t("link.error.recovery.offline") : t("link.error.recovery.premium")}
      </p>
      <div className="link-recover">
        {info.recover === "offline" ? (
          <>
            <Link to={recoverTo} style={{ textDecoration: "none" }}>
              <Button variant="primary" size="lg" block data-testid="link-failure-cta">
                {t("common.signIn")}
              </Button>
            </Link>
            <p className="link-recover-note">{t("link.recovery.offline.note")}</p>
          </>
        ) : (
          <>
            <Alert tone="info">
              <p>{t("link.recovery.premium.note")}</p>
            </Alert>
            <Link to={recoverTo} style={{ textDecoration: "none" }}>
              <Button variant="secondary" size="lg" block data-testid="link-failure-cta">
                {t("common.signIn")}
              </Button>
            </Link>
          </>
        )}
      </div>
      <p className="link-errcode mono">portal.link_{code}</p>
    </div>
  );
}
