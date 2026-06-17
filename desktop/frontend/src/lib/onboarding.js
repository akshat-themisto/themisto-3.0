export const DESKTOP_ONBOARDING_STORAGE_KEY = 'themisto-desktop-onboarding-v1';
export const DESKTOP_ONBOARDING_EVENT = 'themisto:desktop-onboarding:start';

export function triggerDesktopOnboarding() {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.removeItem(DESKTOP_ONBOARDING_STORAGE_KEY);
  } catch {
    // noop
  }
  window.dispatchEvent(new window.CustomEvent(DESKTOP_ONBOARDING_EVENT));
}
