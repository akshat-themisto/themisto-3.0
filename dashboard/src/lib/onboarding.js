export const DASHBOARD_ONBOARDING_STORAGE_KEY = 'themisto-dashboard-onboarding-v1';
export const DASHBOARD_ONBOARDING_EVENT = 'themisto:dashboard-onboarding:start';

export function triggerDashboardOnboarding() {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.removeItem(DASHBOARD_ONBOARDING_STORAGE_KEY);
  } catch {
    // noop
  }
  window.dispatchEvent(new window.CustomEvent(DASHBOARD_ONBOARDING_EVENT));
}
