export namespace cursorcleanup {
	
	export class Finding {
	    source: string;
	    file_path: string;
	    key?: string;
	    pattern_name: string;
	    pattern_type: string;
	    excerpt: string;
	    match_count: number;
	
	    static createFrom(source: any = {}) {
	        return new Finding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.file_path = source["file_path"];
	        this.key = source["key"];
	        this.pattern_name = source["pattern_name"];
	        this.pattern_type = source["pattern_type"];
	        this.excerpt = source["excerpt"];
	        this.match_count = source["match_count"];
	    }
	}
	export class Preview {
	    database_count: number;
	    transcript_count: number;
	    scan_available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Preview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.database_count = source["database_count"];
	        this.transcript_count = source["transcript_count"];
	        this.scan_available = source["scan_available"];
	    }
	}
	export class Result {
	    success: boolean;
	    message: string;
	    databases_found: number;
	    transcripts_found: number;
	    total_findings: number;
	    redacted_count: number;
	    findings: Finding[];
	    errors?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.databases_found = source["databases_found"];
	        this.transcripts_found = source["transcripts_found"];
	        this.total_findings = source["total_findings"];
	        this.redacted_count = source["redacted_count"];
	        this.findings = this.convertValues(source["findings"], Finding);
	        this.errors = source["errors"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class ActionResult {
	    success: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ActionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	    }
	}
	export class AdminGuideItem {
	    title: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new AdminGuideItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.detail = source["detail"];
	    }
	}
	export class AdminPolicyCheck {
	    browser: string;
	    check: string;
	    location: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new AdminPolicyCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.browser = source["browser"];
	        this.check = source["check"];
	        this.location = source["location"];
	        this.detail = source["detail"];
	    }
	}
	export class AdminStatusDefinition {
	    status: string;
	    label: string;
	    meaning: string;
	    admin_action: string;
	
	    static createFrom(source: any = {}) {
	        return new AdminStatusDefinition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.label = source["label"];
	        this.meaning = source["meaning"];
	        this.admin_action = source["admin_action"];
	    }
	}
	export class AdminPolicyGuide {
	    supported_browsers: string[];
	    deployment_methods: AdminGuideItem[];
	    local_checks: AdminPolicyCheck[];
	    browser_status_definitions: AdminStatusDefinition[];
	    health_status_definitions: AdminStatusDefinition[];
	    audit_visibility: AdminGuideItem[];
	    limitations: string[];
	    troubleshooting: AdminGuideItem[];
	    employee_messaging: AdminGuideItem[];
	
	    static createFrom(source: any = {}) {
	        return new AdminPolicyGuide(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported_browsers = source["supported_browsers"];
	        this.deployment_methods = this.convertValues(source["deployment_methods"], AdminGuideItem);
	        this.local_checks = this.convertValues(source["local_checks"], AdminPolicyCheck);
	        this.browser_status_definitions = this.convertValues(source["browser_status_definitions"], AdminStatusDefinition);
	        this.health_status_definitions = this.convertValues(source["health_status_definitions"], AdminStatusDefinition);
	        this.audit_visibility = this.convertValues(source["audit_visibility"], AdminGuideItem);
	        this.limitations = source["limitations"];
	        this.troubleshooting = this.convertValues(source["troubleshooting"], AdminGuideItem);
	        this.employee_messaging = this.convertValues(source["employee_messaging"], AdminGuideItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class AgentConfig {
	    listen_addr: string;
	    gateway_url: string;
	    enrolled: boolean;
	    org_name: string;
	    agent_id: string;
	    enrollment_health: string;
	    cert_expiry?: string;
	    config_issues?: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.listen_addr = source["listen_addr"];
	        this.gateway_url = source["gateway_url"];
	        this.enrolled = source["enrolled"];
	        this.org_name = source["org_name"];
	        this.agent_id = source["agent_id"];
	        this.enrollment_health = source["enrollment_health"];
	        this.cert_expiry = source["cert_expiry"];
	        this.config_issues = source["config_issues"];
	        this.error = source["error"];
	    }
	}
	export class AgentStatus {
	    running: boolean;
	    agent_id: string;
	    gateway_connected: boolean;
	    gateway_state: string;
	    uptime: string;
	    uptime_seconds: number;
	    proxy_state: string;
	    proxy_listener_ready: boolean;
	    runtime_phase: string;
	    version: string;
	    buffer_size: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.agent_id = source["agent_id"];
	        this.gateway_connected = source["gateway_connected"];
	        this.gateway_state = source["gateway_state"];
	        this.uptime = source["uptime"];
	        this.uptime_seconds = source["uptime_seconds"];
	        this.proxy_state = source["proxy_state"];
	        this.proxy_listener_ready = source["proxy_listener_ready"];
	        this.runtime_phase = source["runtime_phase"];
	        this.version = source["version"];
	        this.buffer_size = source["buffer_size"];
	        this.error = source["error"];
	    }
	}
	export class BrowserInfo {
	    name: string;
	    installed: boolean;
	    extension_installed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BrowserInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.installed = source["installed"];
	        this.extension_installed = source["extension_installed"];
	    }
	}
	export class BrowserProtectionStatus {
	    name: string;
	    supported: boolean;
	    installed: boolean;
	    deployment_detected: boolean;
	    protection_status: string;
	    confidence: string;
	    detail: string;
	    remediation: string;
	    last_error?: string;
	
	    static createFrom(source: any = {}) {
	        return new BrowserProtectionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.supported = source["supported"];
	        this.installed = source["installed"];
	        this.deployment_detected = source["deployment_detected"];
	        this.protection_status = source["protection_status"];
	        this.confidence = source["confidence"];
	        this.detail = source["detail"];
	        this.remediation = source["remediation"];
	        this.last_error = source["last_error"];
	    }
	}
	export class ClaudeCodeIntegrationStatus {
	    target?: string;
	    display_name?: string;
	    environment_available: boolean;
	    actions_supported: boolean;
	    settings_path?: string;
	    managed_settings_path?: string;
	    cli_installed: boolean;
	    cli_path?: string;
	    hook_configured: boolean;
	    hook_script_exists: boolean;
	    hook_source?: string;
	    integration_state: string;
	    confidence: string;
	    detail: string;
	    remediation: string;
	    last_error?: string;
	    hooks_disabled?: boolean;
	    managed_override?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ClaudeCodeIntegrationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.display_name = source["display_name"];
	        this.environment_available = source["environment_available"];
	        this.actions_supported = source["actions_supported"];
	        this.settings_path = source["settings_path"];
	        this.managed_settings_path = source["managed_settings_path"];
	        this.cli_installed = source["cli_installed"];
	        this.cli_path = source["cli_path"];
	        this.hook_configured = source["hook_configured"];
	        this.hook_script_exists = source["hook_script_exists"];
	        this.hook_source = source["hook_source"];
	        this.integration_state = source["integration_state"];
	        this.confidence = source["confidence"];
	        this.detail = source["detail"];
	        this.remediation = source["remediation"];
	        this.last_error = source["last_error"];
	        this.hooks_disabled = source["hooks_disabled"];
	        this.managed_override = source["managed_override"];
	    }
	}
	export class ClaudeCodeIntegrationOverview {
	    targets: ClaudeCodeIntegrationStatus[];
	    preferred_target?: string;
	
	    static createFrom(source: any = {}) {
	        return new ClaudeCodeIntegrationOverview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targets = this.convertValues(source["targets"], ClaudeCodeIntegrationStatus);
	        this.preferred_target = source["preferred_target"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class CursorIntegrationStatus {
	    installed: boolean;
	    cli_path?: string;
	    hooks_config_path?: string;
	    enterprise_hooks_path?: string;
	    hook_configured: boolean;
	    hook_script_exists: boolean;
	    hook_source?: string;
	    integration_state: string;
	    confidence: string;
	    detail: string;
	    remediation: string;
	    last_error?: string;
	    enterprise_managed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CursorIntegrationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.cli_path = source["cli_path"];
	        this.hooks_config_path = source["hooks_config_path"];
	        this.enterprise_hooks_path = source["enterprise_hooks_path"];
	        this.hook_configured = source["hook_configured"];
	        this.hook_script_exists = source["hook_script_exists"];
	        this.hook_source = source["hook_source"];
	        this.integration_state = source["integration_state"];
	        this.confidence = source["confidence"];
	        this.detail = source["detail"];
	        this.remediation = source["remediation"];
	        this.last_error = source["last_error"];
	        this.enterprise_managed = source["enterprise_managed"];
	    }
	}
	export class DiagnosticIssue {
	    area: string;
	    title: string;
	    detail: string;
	    remediation: string;
	    severity: string;
	    protection_impact: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.area = source["area"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.remediation = source["remediation"];
	        this.severity = source["severity"];
	        this.protection_impact = source["protection_impact"];
	    }
	}
	export class PathCheck {
	    path: string;
	    label: string;
	    exists: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PathCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.label = source["label"];
	        this.exists = source["exists"];
	    }
	}
	export class WindsurfIntegrationStatus {
	    installed: boolean;
	    cli_path?: string;
	    hooks_config_path?: string;
	    system_hooks_path?: string;
	    hook_configured: boolean;
	    hook_script_exists: boolean;
	    hook_source?: string;
	    integration_state: string;
	    confidence: string;
	    detail: string;
	    remediation: string;
	    last_error?: string;
	    system_managed?: boolean;
	    workspace_managed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WindsurfIntegrationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.cli_path = source["cli_path"];
	        this.hooks_config_path = source["hooks_config_path"];
	        this.system_hooks_path = source["system_hooks_path"];
	        this.hook_configured = source["hook_configured"];
	        this.hook_script_exists = source["hook_script_exists"];
	        this.hook_source = source["hook_source"];
	        this.integration_state = source["integration_state"];
	        this.confidence = source["confidence"];
	        this.detail = source["detail"];
	        this.remediation = source["remediation"];
	        this.last_error = source["last_error"];
	        this.system_managed = source["system_managed"];
	        this.workspace_managed = source["workspace_managed"];
	    }
	}
	export class GitHubCopilotIntegrationStatus {
	    installed: boolean;
	    actions_supported: boolean;
	    cli_path?: string;
	    settings_path?: string;
	    hook_directory_path?: string;
	    hook_config_path?: string;
	    hook_configured: boolean;
	    hook_script_exists: boolean;
	    hook_source?: string;
	    integration_state: string;
	    confidence: string;
	    detail: string;
	    remediation: string;
	    last_error?: string;
	
	    static createFrom(source: any = {}) {
	        return new GitHubCopilotIntegrationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.actions_supported = source["actions_supported"];
	        this.cli_path = source["cli_path"];
	        this.settings_path = source["settings_path"];
	        this.hook_directory_path = source["hook_directory_path"];
	        this.hook_config_path = source["hook_config_path"];
	        this.hook_configured = source["hook_configured"];
	        this.hook_script_exists = source["hook_script_exists"];
	        this.hook_source = source["hook_source"];
	        this.integration_state = source["integration_state"];
	        this.confidence = source["confidence"];
	        this.detail = source["detail"];
	        this.remediation = source["remediation"];
	        this.last_error = source["last_error"];
	    }
	}
	export class SystemInfo {
	    os: string;
	    hostname: string;
	    version: string;
	    go_version: string;
	
	    static createFrom(source: any = {}) {
	        return new SystemInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.os = source["os"];
	        this.hostname = source["hostname"];
	        this.version = source["version"];
	        this.go_version = source["go_version"];
	    }
	}
	export class InstalledState {
	    binary_exists: boolean;
	    config_exists: boolean;
	    config_readable: boolean;
	    config_valid: boolean;
	    has_agent_id: boolean;
	    has_gateway_url: boolean;
	    certs_exist: boolean;
	    service_installed: boolean;
	    summary: string;
	
	    static createFrom(source: any = {}) {
	        return new InstalledState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.binary_exists = source["binary_exists"];
	        this.config_exists = source["config_exists"];
	        this.config_readable = source["config_readable"];
	        this.config_valid = source["config_valid"];
	        this.has_agent_id = source["has_agent_id"];
	        this.has_gateway_url = source["has_gateway_url"];
	        this.certs_exist = source["certs_exist"];
	        this.service_installed = source["service_installed"];
	        this.summary = source["summary"];
	    }
	}
	export class DiagnosticsSnapshot {
	    timestamp: string;
	    app_version: string;
	    summary_status: string;
	    status: AgentStatus;
	    config: AgentConfig;
	    installed: InstalledState;
	    system: SystemInfo;
	    browsers: BrowserInfo[];
	    browser_protection: BrowserProtectionStatus[];
	    claude_code: ClaudeCodeIntegrationStatus;
	    cursor: CursorIntegrationStatus;
	    github_copilot: GitHubCopilotIntegrationStatus;
	    windsurf: WindsurfIntegrationStatus;
	    path_checks: PathCheck[];
	    service_state: string;
	    service_detail: string;
	    issues: DiagnosticIssue[];
	    advisory_count: number;
	    collection_errors?: string[];
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticsSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.app_version = source["app_version"];
	        this.summary_status = source["summary_status"];
	        this.status = this.convertValues(source["status"], AgentStatus);
	        this.config = this.convertValues(source["config"], AgentConfig);
	        this.installed = this.convertValues(source["installed"], InstalledState);
	        this.system = this.convertValues(source["system"], SystemInfo);
	        this.browsers = this.convertValues(source["browsers"], BrowserInfo);
	        this.browser_protection = this.convertValues(source["browser_protection"], BrowserProtectionStatus);
	        this.claude_code = this.convertValues(source["claude_code"], ClaudeCodeIntegrationStatus);
	        this.cursor = this.convertValues(source["cursor"], CursorIntegrationStatus);
	        this.github_copilot = this.convertValues(source["github_copilot"], GitHubCopilotIntegrationStatus);
	        this.windsurf = this.convertValues(source["windsurf"], WindsurfIntegrationStatus);
	        this.path_checks = this.convertValues(source["path_checks"], PathCheck);
	        this.service_state = source["service_state"];
	        this.service_detail = source["service_detail"];
	        this.issues = this.convertValues(source["issues"], DiagnosticIssue);
	        this.advisory_count = source["advisory_count"];
	        this.collection_errors = source["collection_errors"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EnrollResult {
	    success: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new EnrollResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	    }
	}
	export class EnrollmentDefaults {
	    token: string;
	    org_name: string;
	    device_id: string;
	    backend_url: string;
	    gateway_url: string;
	
	    static createFrom(source: any = {}) {
	        return new EnrollmentDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.token = source["token"];
	        this.org_name = source["org_name"];
	        this.device_id = source["device_id"];
	        this.backend_url = source["backend_url"];
	        this.gateway_url = source["gateway_url"];
	    }
	}
	export class EnrollmentImportResult {
	    success: boolean;
	    message: string;
	    source?: string;
	    defaults?: EnrollmentDefaults;
	
	    static createFrom(source: any = {}) {
	        return new EnrollmentImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.source = source["source"];
	        this.defaults = this.convertValues(source["defaults"], EnrollmentDefaults);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExportResult {
	    success: boolean;
	    path?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.path = source["path"];
	        this.error = source["error"];
	    }
	}
	
	
	
	export class ProxyRecoveryStatus {
	    should_offer: boolean;
	    needs_elevation: boolean;
	    reason: string;
	    proxy_host: string;
	    proxy_port: number;
	
	    static createFrom(source: any = {}) {
	        return new ProxyRecoveryStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.should_offer = source["should_offer"];
	        this.needs_elevation = source["needs_elevation"];
	        this.reason = source["reason"];
	        this.proxy_host = source["proxy_host"];
	        this.proxy_port = source["proxy_port"];
	    }
	}
	export class ResetProxyResult {
	    success: boolean;
	    partial: boolean;
	    pending: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ResetProxyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.partial = source["partial"];
	        this.pending = source["pending"];
	        this.message = source["message"];
	    }
	}
	

}

