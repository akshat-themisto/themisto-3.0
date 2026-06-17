import { useEffect, useMemo, useState } from 'react';
import { ArrowRight, Sparkles, X } from 'lucide-react';
import { useLocation, useNavigate } from 'react-router-dom';
import { DESKTOP_ONBOARDING_EVENT, DESKTOP_ONBOARDING_STORAGE_KEY } from '../lib/onboarding';

const TOUR_STEPS = [
  {
    selector: '[data-tour="desktop-brand"]',
    eyebrow: 'Workspace shell',
    title: 'This is the local Themisto workspace.',
    description: 'Your current mode, the app identity, and the device session all stay visible here so the desktop shell feels grounded.',
  },
  {
    selector: '[data-tour="desktop-sidebar"]',
    eyebrow: 'Navigation',
    title: 'Core pages stay in the left rail.',
    description: 'Runtime, browser coverage, device details, and admin mode are always one move away from here.',
  },
  {
    selector: '[data-tour="desktop-runtime-summary"]',
    eyebrow: 'Runtime',
    title: 'Home gives you the current health of the device.',
    description: 'This is the fastest place to see enrollment state, gateway reachability, and whether protection is behaving the way you expect.',
  },
  {
    selector: '[data-tour="desktop-runtime-table"]',
    eyebrow: 'Subsystems',
    title: 'Use the runtime table when you need specifics.',
    description: 'It breaks the agent down into service, gateway, proxy, and enrollment so troubleshooting does not feel like guesswork.',
  },
  {
    selector: '[data-tour="desktop-admin-toggle"]',
    eyebrow: 'Admin mode',
    title: 'Admin mode stays explicit on purpose.',
    description: 'Unlock it only when you need deeper troubleshooting or policy controls, then lock it again when you are done.',
  },
  {
    selector: '[data-tour="desktop-statusbar"]',
    eyebrow: 'Live state',
    title: 'The status bar is your instant confidence check.',
    description: 'It keeps agent state, gateway connectivity, buffer size, and app version visible without leaving the page.',
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
      const completed = window.localStorage.getItem(DESKTOP_ONBOARDING_STORAGE_KEY);
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
    window.addEventListener(DESKTOP_ONBOARDING_EVENT, onEvent);
    return () => window.removeEventListener(DESKTOP_ONBOARDING_EVENT, onEvent);
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
      window.localStorage.setItem(DESKTOP_ONBOARDING_STORAGE_KEY, 'done');
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
      <div className="desktop-onboarding-overlay">
        <div className="desktop-welcome-shell">
          <div className="desktop-welcome-copy">
            <div className="desktop-welcome-kicker">Welcome to Themisto</div>
            <h2 className="desktop-welcome-title">A guided start for Themisto Labs.</h2>
            <p className="desktop-welcome-text">
              We'll show you runtime health, browser coverage, device details, and admin controls so the app feels familiar from the first launch.
            </p>
            <div className="desktop-welcome-actions">
              <button className="btn btn-primary" type="button" onClick={startTour}>
                Start quick tour
                <ArrowRight size={14} />
              </button>
              <button className="btn" type="button" onClick={finishTour}>
                Skip for now
              </button>
            </div>
          </div>

          <div className="desktop-welcome-visual">
            <div className="desktop-onboarding-orbit desktop-onboarding-orbit-outer" />
            <div className="desktop-onboarding-orbit desktop-onboarding-orbit-inner" />
            <div className="desktop-onboarding-stage">
              <div className="desktop-onboarding-rotation">
                <img src="/transparent-logo.png" alt="Themisto Labs" className="desktop-onboarding-logo" />
              </div>
            </div>
            <div className="desktop-onboarding-feature-card">
              <div className="desktop-onboarding-feature-label">Desktop guide</div>
              <strong>We will cover runtime health, enrollment state, browser protection, and how admin mode fits into the endpoint desktop flow.</strong>
            </div>
          </div>

          <button className="desktop-onboarding-close" type="button" onClick={finishTour} aria-label="Close welcome guide">
            <X size={16} />
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="desktop-tour-overlay">
      {targetRect && (
        <div
          className="desktop-tour-highlight"
          style={{
            top: targetRect.top,
            left: targetRect.left,
            width: targetRect.width,
            height: targetRect.height,
          }}
        />
      )}

      <div
        className="desktop-tour-card"
        style={{
          top: cardPosition.top,
          left: cardPosition.left,
          width: cardPosition.width,
        }}
      >
        <div className="desktop-tour-kicker">
          <Sparkles size={14} />
          {activeStep?.eyebrow}
        </div>
        <h3 className="desktop-tour-title">{activeStep?.title}</h3>
        <p className="desktop-tour-copy">{activeStep?.description}</p>

        <div className="desktop-tour-progress">
          {TOUR_STEPS.map((_, index) => (
            <span key={index} className={`desktop-tour-progress-dot${index === stepIndex ? ' active' : ''}`} />
          ))}
        </div>

        <div className="desktop-tour-actions">
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
