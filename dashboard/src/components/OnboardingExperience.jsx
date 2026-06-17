import { useEffect, useMemo, useState } from 'react';
import { ArrowRight, Sparkles, X } from 'lucide-react';
import { useLocation, useNavigate } from 'react-router-dom';
import { DASHBOARD_ONBOARDING_EVENT, DASHBOARD_ONBOARDING_STORAGE_KEY } from '../lib/onboarding';

const TOUR_STEPS = [
  {
    selector: '[data-tour="dashboard-brand"]',
    eyebrow: 'Workspace shell',
    title: 'This is your security command center.',
    description: 'The header keeps the workspace identity and your current role visible so the console stays anchored.',
  },
  {
    selector: '[data-tour="dashboard-sidebar"]',
    eyebrow: 'Navigation',
    title: 'Everything important lives in the left rail.',
    description: 'Overview, devices, signals, policies, activity, and settings are all one click away from here.',
  },
  {
    selector: '[data-tour="dashboard-overview-hero"]',
    eyebrow: 'Live security posture',
    title: 'Overview gives you the current operating picture.',
    description: 'This panel is the fastest way to understand endpoint activity, trust health, and whether policy is behaving the way you expect.',
  },
  {
    selector: '[data-tour="dashboard-stats-grid"]',
    eyebrow: 'Signals',
    title: 'These cards stay close to the metrics that matter.',
    description: 'Think of them as your daily heartbeat: active endpoints, trust posture, AI sessions, and security interventions.',
  },
  {
    selector: '[data-tour="dashboard-alerts"]',
    eyebrow: 'Attention needed',
    title: 'Alerts surface the items that deserve a closer look.',
    description: 'Use this bell when you want to jump straight to noteworthy events without scanning the whole dashboard.',
  },
  {
    selector: '[data-tour="dashboard-nav-settings"]',
    eyebrow: 'Controls',
    title: 'Settings is where you manage the workspace.',
    description: 'Workspace details, operators, password rotation, exports, and tour replay all live there.',
  },
];

function clamp(value, min, max) {
  return Math.max(min, Math.min(max, value));
}

function getPopoverPosition(rect) {
  if (!rect) {
    return { top: 24, left: 24, width: 340 };
  }

  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const width = Math.min(360, viewportWidth - 32);
  const gap = 18;
  const preferredHeight = 260;

  let left = rect.right + gap;
  let top = rect.top;

  if (left + width > viewportWidth - 24) {
    left = rect.left - width - gap;
  }
  if (left < 24) {
    left = clamp(rect.left, 24, viewportWidth - width - 24);
    top = rect.bottom + gap;
  }
  if (top + preferredHeight > viewportHeight - 24) {
    top = rect.top - preferredHeight - gap;
  }
  if (top < 24) {
    top = clamp(rect.bottom + gap, 24, viewportHeight - preferredHeight - 24);
  }

  return { top, left, width };
}

export default function OnboardingExperience({ disabled = false }) {
  const location = useLocation();
  const navigate = useNavigate();
  const [mode, setMode] = useState(null);
  const [pendingStart, setPendingStart] = useState(false);
  const [stepIndex, setStepIndex] = useState(0);
  const [targetRect, setTargetRect] = useState(null);

  const activeStep = TOUR_STEPS[stepIndex] || null;
  const cardPosition = useMemo(() => getPopoverPosition(targetRect), [targetRect]);

  useEffect(() => {
    if (disabled) return;
    try {
      const completed = window.localStorage.getItem(DASHBOARD_ONBOARDING_STORAGE_KEY);
      if (!completed) {
        setMode('welcome');
      }
    } catch {
      setMode('welcome');
    }
  }, [disabled]);

  useEffect(() => {
    if (disabled) {
      setMode(null);
      return;
    }

    const start = () => {
      if (location.pathname !== '/') {
        navigate('/');
        setPendingStart(true);
      } else {
        setStepIndex(0);
        setMode('tour');
      }
    };

    const onEvent = () => start();
    window.addEventListener(DASHBOARD_ONBOARDING_EVENT, onEvent);
    return () => window.removeEventListener(DASHBOARD_ONBOARDING_EVENT, onEvent);
  }, [disabled, location.pathname, navigate]);

  useEffect(() => {
    if (!pendingStart) return;
    if (location.pathname === '/') {
      setPendingStart(false);
      setStepIndex(0);
      setMode('tour');
    }
  }, [pendingStart, location.pathname]);

  useEffect(() => {
    if (mode !== 'tour' || !activeStep) return;

    let raf = 0;
    let interval = 0;

    const updateRect = () => {
      const element = document.querySelector(activeStep.selector);
      if (!element) {
        setTargetRect(null);
        return;
      }
      element.scrollIntoView({ block: 'center', behavior: 'smooth' });
      const rect = element.getBoundingClientRect();
      setTargetRect({
        top: rect.top - 8,
        left: rect.left - 8,
        width: rect.width + 16,
        height: rect.height + 16,
        right: rect.right + 8,
        bottom: rect.bottom + 8,
      });
    };

    const schedule = () => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(updateRect);
    };

    const timeout = window.setTimeout(schedule, 180);
    interval = window.setInterval(schedule, 260);
    window.addEventListener('resize', schedule);
    window.addEventListener('scroll', schedule, true);

    return () => {
      window.clearTimeout(timeout);
      window.clearInterval(interval);
      cancelAnimationFrame(raf);
      window.removeEventListener('resize', schedule);
      window.removeEventListener('scroll', schedule, true);
    };
  }, [mode, activeStep]);

  function persistComplete() {
    try {
      window.localStorage.setItem(DASHBOARD_ONBOARDING_STORAGE_KEY, 'done');
    } catch {
      // noop
    }
  }

  function finishTour() {
    persistComplete();
    setMode(null);
    setTargetRect(null);
    setPendingStart(false);
  }

  function startTour() {
    if (location.pathname !== '/') {
      navigate('/');
      setPendingStart(true);
    } else {
      setStepIndex(0);
      setMode('tour');
    }
  }

  if (disabled || !mode) {
    return null;
  }

  if (mode === 'welcome') {
    return (
      <div className="onboarding-overlay">
        <div className="onboarding-welcome-shell">
          <div className="onboarding-welcome-copy">
            <div className="onboarding-welcome-kicker">Welcome to Themisto Labs</div>
            <h2 className="onboarding-welcome-title">A calmer way to see endpoints, AI use, policy, and DLP events in one place.</h2>
            <p className="onboarding-welcome-text">
              We'll show you the core surfaces first so the workspace feels familiar immediately, then you can get back to protecting users and data.
            </p>
            <div className="onboarding-welcome-actions">
              <button className="btn btn-primary" type="button" onClick={startTour}>
                Start quick tour
                <ArrowRight size={14} />
              </button>
              <button className="btn" type="button" onClick={finishTour}>
                Skip for now
              </button>
            </div>
          </div>

          <div className="onboarding-welcome-hero">
            <div className="onboarding-orbit onboarding-orbit-outer" />
            <div className="onboarding-orbit onboarding-orbit-inner" />
            <div className="onboarding-logo-stage">
              <div className="onboarding-logo-rotation">
                <img src="/logo.png" alt="Themisto Labs" className="onboarding-logo" />
              </div>
            </div>
            <div className="onboarding-hero-card">
              <div className="onboarding-hero-label">First-run guide</div>
              <strong>We'll walk through the dashboard shell, live security posture, alerts, and workspace controls.</strong>
            </div>
          </div>

          <button className="onboarding-close" type="button" onClick={finishTour} aria-label="Close welcome guide">
            <X size={16} />
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="tour-overlay">
      {targetRect && (
        <div
          className="tour-highlight"
          style={{
            top: targetRect.top,
            left: targetRect.left,
            width: targetRect.width,
            height: targetRect.height,
          }}
        />
      )}

      <div
        className="tour-card"
        style={{
          top: cardPosition.top,
          left: cardPosition.left,
          width: cardPosition.width,
        }}
      >
        <div className="tour-card-kicker">
          <Sparkles size={14} />
          {activeStep?.eyebrow}
        </div>
        <h3 className="tour-card-title">{activeStep?.title}</h3>
        <p className="tour-card-copy">{activeStep?.description}</p>

        <div className="tour-progress">
          {TOUR_STEPS.map((_, index) => (
            <span key={index} className={`tour-progress-dot${index === stepIndex ? ' active' : ''}`} />
          ))}
        </div>

        <div className="tour-card-actions">
          <button className="btn" type="button" onClick={finishTour}>
            Finish later
          </button>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn" type="button" disabled={stepIndex === 0} onClick={() => setStepIndex((current) => Math.max(0, current - 1))}>
              Back
            </button>
            <button
              className="btn btn-primary"
              type="button"
              onClick={() => {
                if (stepIndex >= TOUR_STEPS.length - 1) {
                  finishTour();
                } else {
                  setStepIndex((current) => current + 1);
                }
              }}
            >
              {stepIndex >= TOUR_STEPS.length - 1 ? 'Finish' : 'Next'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
