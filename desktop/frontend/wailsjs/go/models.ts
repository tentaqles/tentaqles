export namespace main {
	
	export class BoardTarget {
	    URL: string;
	    Started: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BoardTarget(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.URL = source["URL"];
	        this.Started = source["Started"];
	    }
	}
	export class GuardChange {
	    Workspace: string;
	    Scope: string;
	    Looser: boolean;
	    Proposal: string;
	    Carried: boolean;
	    Dropped: string;
	    Warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new GuardChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Workspace = source["Workspace"];
	        this.Scope = source["Scope"];
	        this.Looser = source["Looser"];
	        this.Proposal = source["Proposal"];
	        this.Carried = source["Carried"];
	        this.Dropped = source["Dropped"];
	        this.Warnings = source["Warnings"];
	    }
	}

}

export namespace setup {
	
	export class GuardCategory {
	    name: string;
	    value: string;
	    source: string;
	    locked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GuardCategory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.source = source["source"];
	        this.locked = source["locked"];
	    }
	}
	export class GuardOverride {
	    id: string;
	    value: string;
	    source?: string;
	
	    static createFrom(source: any = {}) {
	        return new GuardOverride(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.value = source["value"];
	        this.source = source["source"];
	    }
	}
	export class GuardView {
	    workspace?: string;
	    root?: string;
	    mode: string;
	    label: string;
	    base?: string;
	    requested?: string;
	    mode_source?: string;
	    categories: GuardCategory[];
	    overrides: GuardOverride[];
	    ceiling: string;
	    granted: boolean;
	    granted_at?: string;
	    looser_than_default: boolean;
	    needs_grant: boolean;
	    proposal?: string;
	    clamped: string[];
	    ignored: string[];
	    skipped_confirms_24h: number;
	    skipped_blocks_24h: number;
	    locked: string[];
	    projects: string[];
	
	    static createFrom(source: any = {}) {
	        return new GuardView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspace = source["workspace"];
	        this.root = source["root"];
	        this.mode = source["mode"];
	        this.label = source["label"];
	        this.base = source["base"];
	        this.requested = source["requested"];
	        this.mode_source = source["mode_source"];
	        this.categories = this.convertValues(source["categories"], GuardCategory);
	        this.overrides = this.convertValues(source["overrides"], GuardOverride);
	        this.ceiling = source["ceiling"];
	        this.granted = source["granted"];
	        this.granted_at = source["granted_at"];
	        this.looser_than_default = source["looser_than_default"];
	        this.needs_grant = source["needs_grant"];
	        this.proposal = source["proposal"];
	        this.clamped = source["clamped"];
	        this.ignored = source["ignored"];
	        this.skipped_confirms_24h = source["skipped_confirms_24h"];
	        this.skipped_blocks_24h = source["skipped_blocks_24h"];
	        this.locked = source["locked"];
	        this.projects = source["projects"];
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

export namespace setupapi {
	
	export class Change {
	    Kind: string;
	    Target: string;
	    Detail: string;
	
	    static createFrom(source: any = {}) {
	        return new Change(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Kind = source["Kind"];
	        this.Target = source["Target"];
	        this.Detail = source["Detail"];
	    }
	}
	export class Tracker {
	    Provider: string;
	    Scope: Record<string, string>;
	    AssignedTo: string;
	    Auth: string;
	    Env: string;
	    Lanes: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Tracker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Provider = source["Provider"];
	        this.Scope = source["Scope"];
	        this.AssignedTo = source["AssignedTo"];
	        this.Auth = source["Auth"];
	        this.Env = source["Env"];
	        this.Lanes = source["Lanes"];
	    }
	}
	export class Company {
	    Name: string;
	    DisplayName: string;
	    Color: string;
	    GitName: string;
	    GitEmail: string;
	    GitUser: string;
	    GitProvider: string;
	    Identities: string[];
	    PermissionMode: string;
	    Tracker?: Tracker;
	    Chat: string;
	    Email: string;
	
	    static createFrom(source: any = {}) {
	        return new Company(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.DisplayName = source["DisplayName"];
	        this.Color = source["Color"];
	        this.GitName = source["GitName"];
	        this.GitEmail = source["GitEmail"];
	        this.GitUser = source["GitUser"];
	        this.GitProvider = source["GitProvider"];
	        this.Identities = source["Identities"];
	        this.PermissionMode = source["PermissionMode"];
	        this.Tracker = this.convertValues(source["Tracker"], Tracker);
	        this.Chat = source["Chat"];
	        this.Email = source["Email"];
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
	export class Finding {
	    Level: string;
	    Code: string;
	    Workspace: string;
	    Msg: string;
	    Fix: string;
	
	    static createFrom(source: any = {}) {
	        return new Finding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Level = source["Level"];
	        this.Code = source["Code"];
	        this.Workspace = source["Workspace"];
	        this.Msg = source["Msg"];
	        this.Fix = source["Fix"];
	    }
	}
	export class FolderCandidate {
	    name: string;
	    path: string;
	    managed: boolean;
	    repos: number;
	    gitName: string;
	    gitEmail: string;
	
	    static createFrom(source: any = {}) {
	        return new FolderCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.managed = source["managed"];
	        this.repos = source["repos"];
	        this.gitName = source["gitName"];
	        this.gitEmail = source["gitEmail"];
	    }
	}
	export class HookStatus {
	    Shell: string;
	    Profile: string;
	    State: string;
	
	    static createFrom(source: any = {}) {
	        return new HookStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Shell = source["Shell"];
	        this.Profile = source["Profile"];
	        this.State = source["State"];
	    }
	}
	export class Hub {
	    URL: string;
	    Port: number;
	    PID: number;
	    Mode: string;
	    Workspaces: string[];
	
	    static createFrom(source: any = {}) {
	        return new Hub(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.URL = source["URL"];
	        this.Port = source["Port"];
	        this.PID = source["PID"];
	        this.Mode = source["Mode"];
	        this.Workspaces = source["Workspaces"];
	    }
	}
	export class Plan {
	    Base: string;
	    Companies: Company[];
	    Hooks: string[];
	    Trust: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Base = source["Base"];
	        this.Companies = this.convertValues(source["Companies"], Company);
	        this.Hooks = source["Hooks"];
	        this.Trust = source["Trust"];
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
	export class Provider {
	    ID: string;
	    Name: string;
	    Category: string;
	    HasCLI: boolean;
	    HasIdentity: boolean;
	    Docs: string;
	
	    static createFrom(source: any = {}) {
	        return new Provider(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Category = source["Category"];
	        this.HasCLI = source["HasCLI"];
	        this.HasIdentity = source["HasIdentity"];
	        this.Docs = source["Docs"];
	    }
	}
	export class Report {
	    Changes: Change[];
	    Logins: string[];
	    Warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Changes = this.convertValues(source["Changes"], Change);
	        this.Logins = source["Logins"];
	        this.Warnings = source["Warnings"];
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
	export class Services {
	    Trackers: string[];
	    TrackerAuths: string[];
	    Assignees: string[];
	    Chat: string[];
	    Email: string[];
	
	    static createFrom(source: any = {}) {
	        return new Services(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Trackers = source["Trackers"];
	        this.TrackerAuths = source["TrackerAuths"];
	        this.Assignees = source["Assignees"];
	        this.Chat = source["Chat"];
	        this.Email = source["Email"];
	    }
	}
	
	export class Workspace {
	    Name: string;
	    Root: string;
	    Email: string;
	    Trusted: boolean;
	    Identities: string[];
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Root = source["Root"];
	        this.Email = source["Email"];
	        this.Trusted = source["Trusted"];
	        this.Identities = source["Identities"];
	    }
	}

}

