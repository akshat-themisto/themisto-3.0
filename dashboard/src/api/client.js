export const API_BASE_URL = import.meta.env.VITE_API_URL || '';

export class ApiError extends Error {
    constructor(message, { status, code, data } = {}) {
        super(message);
        this.name = 'ApiError';
        this.status = status;
        this.code = code;
        this.data = data;
    }
}

function redirectTo(path) {
    if (typeof window === 'undefined') return;
    if (window.location.pathname !== path) {
        window.location.replace(path);
    }
}

async function request(path, options = {}) {
    const url = `${API_BASE_URL}${path}`;
    const { suppressAuthRedirect = false, headers = {}, ...fetchOptions } = options;
    const config = {
        credentials: 'include',
        ...fetchOptions,
        headers: { 'Content-Type': 'application/json', ...headers },
    };

    const res = await fetch(url, config);
    const data = await res.json().catch(() => null);

    if (res.status === 401) {
        if (!suppressAuthRedirect) {
            redirectTo('/login');
        }
        throw new ApiError(data?.error || 'Unauthorized', {
            status: res.status,
            code: data?.code,
            data,
        });
    }

    if (res.status === 403 && data?.code === 'PASSWORD_CHANGE_REQUIRED') {
        redirectTo('/settings');
    }

    if (!res.ok) {
        throw new ApiError(data?.error || `Request failed: ${res.status}`, {
            status: res.status,
            code: data?.code,
            data,
        });
    }
    return data;
}

export const api = {
    // Auth
    login: (email, password) => request('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email, password }) }),
    logout: () => request('/api/v1/auth/logout', { method: 'POST' }),
    me: () => request('/api/v1/auth/me', { suppressAuthRedirect: true }),
    acceptTerms: () => request('/api/v1/auth/terms/accept', { method: 'POST' }),

    // Dashboard
    stats: () => request('/api/v1/dashboard/stats'),

    // Devices
    listDevices: (orgId, status) => request(`/api/v1/orgs/${orgId}/devices${status ? `?status=${status}` : ''}`),
    createEnrollmentPackage: (data) => request('/api/v1/devices/enrollment-package', { method: 'POST', body: JSON.stringify(data) }),
    reissueEnrollmentToken: (deviceId) => request(`/api/v1/devices/${deviceId}/enrollment-token`, { method: 'POST' }),

    // Audit
    listAudit: (params = {}) => {
        const qs = new URLSearchParams(Object.fromEntries(Object.entries(params).filter(([, v]) => v))).toString();
        return request(`/api/v1/audit-log?${qs}`);
    },

    // Telemetry
    listTelemetry: (params = {}) => {
        const qs = new URLSearchParams(Object.fromEntries(Object.entries(params).filter(([, v]) => v))).toString();
        return request(`/api/v1/telemetry/events?${qs}`);
    },
    telemetryTimeSeries: (params = {}) => {
        const qs = new URLSearchParams(Object.fromEntries(Object.entries(params).filter(([, v]) => v))).toString();
        return request(`/api/v1/telemetry/timeseries?${qs}`);
    },

    // Policies (v2)
    listPolicies: () => request('/api/v1/policies'),
    createPolicy: (data) => request('/api/v1/policies', { method: 'POST', body: JSON.stringify(data) }),
    updatePolicy: (id, data) => request(`/api/v1/policies/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    deletePolicy: (id) => request(`/api/v1/policies/${id}`, { method: 'DELETE' }),
    testPolicy: (data) => request('/api/v1/policies/test', { method: 'POST', body: JSON.stringify(data) }),

    // AI governance
    listAIGovernanceVendors: async () => {
        try {
            return await request('/api/v1/ai-governance/vendors');
        } catch (err) {
            if ((err.message || '').includes('404')) {
                return request('/api/v1/ai/governance/vendors');
            }
            throw err;
        }
    },
    listAIInterceptDomains: () => request('/api/v1/ai/intercept-domains'),
    updateAIInterceptDomains: (domains) => request('/api/v1/ai/intercept-domains', { method: 'PUT', body: JSON.stringify({ domains }) }),
    updateAIGovernanceVendor: async (vendor, data) => {
        const encoded = encodeURIComponent(vendor);
        try {
            return await request(`/api/v1/ai-governance/vendors/${encoded}`, { method: 'PUT', body: JSON.stringify(data) });
        } catch (err) {
            if ((err.message || '').includes('404')) {
                return request(`/api/v1/ai/governance/vendors/${encoded}`, { method: 'POST', body: JSON.stringify(data) });
            }
            throw err;
        }
    },
    blockUnsanctionedVendor: async (vendor) => {
        const encoded = encodeURIComponent(vendor);
        try {
            return await request(`/api/v1/ai-governance/vendors/${encoded}/block`, { method: 'POST' });
        } catch (err) {
            if ((err.message || '').includes('404')) {
                return request(`/api/v1/ai/governance/vendors/${encoded}/block`, { method: 'POST' });
            }
            throw err;
        }
    },
    unblockVendor: async (vendor) => {
        const encoded = encodeURIComponent(vendor);
        try {
            return await request(`/api/v1/ai-governance/vendors/${encoded}/unblock`, { method: 'POST' });
        } catch (err) {
            if ((err.message || '').includes('404')) {
                return request(`/api/v1/ai/governance/vendors/${encoded}/unblock`, { method: 'POST' });
            }
            throw err;
        }
    },

    // Settings
    getSettings: () => request('/api/v1/settings'),
    updateSettings: (data) => request('/api/v1/settings', { method: 'PUT', body: JSON.stringify(data) }),
    createUser: (data) => request('/api/v1/users', { method: 'POST', body: JSON.stringify(data) }),
    deleteUser: (id) => request(`/api/v1/users/${id}`, { method: 'DELETE' }),
    changePassword: (data) => request('/api/v1/auth/password', { method: 'PUT', body: JSON.stringify(data) }),

    // AI Usage
    aiUsage: (params = {}) => {
        const qs = new URLSearchParams(Object.fromEntries(Object.entries(params).filter(([, v]) => v))).toString();
        return request(`/api/v1/ai-usage?${qs}`);
    },

    // DLP Events
    listDLPEvents: (params = {}) => {
        const qs = new URLSearchParams(Object.fromEntries(Object.entries(params).filter(([, v]) => v))).toString();
        return request(`/api/v1/dlp/events?${qs}`);
    },
    getDLPEvent: (id) => request(`/api/v1/dlp/events/${id}`),
    dlpSummary: (params = {}) => {
        const qs = new URLSearchParams(Object.fromEntries(Object.entries(params).filter(([, v]) => v))).toString();
        return request(`/api/v1/dlp/summary?${qs}`);
    },
    getDLPEventBody: (id) => request(`/api/v1/dlp/events/${id}/body`),
    listAlerts: (params = {}) => {
        const qs = new URLSearchParams(Object.fromEntries(Object.entries(params).filter(([, v]) => v))).toString();
        return request(`/api/v1/alerts?${qs}`);
    },
    markAlertsRead: (eventIDs = []) => request('/api/v1/alerts/read', { method: 'POST', body: JSON.stringify({ event_ids: eventIDs }) }),

    // Evidence exports
    evidencePolicySnapshot: () => request('/api/v1/evidence/policies'),
    evidenceDLPCSVUrl: () => `${API_BASE_URL}/api/v1/evidence/dlp.csv`,
    evidenceAuditCSVUrl: () => `${API_BASE_URL}/api/v1/evidence/audit.csv`,

};
