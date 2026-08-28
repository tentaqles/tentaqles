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
	export class Company {
	    Name: string;
	    DisplayName: string;
	    Color: string;
	    GitName: string;
	    GitEmail: string;
	    GitUser: string;
	    Identities: string[];
	    PermissionMode: string;
	
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
	        this.Identities = source["Identities"];
	        this.PermissionMode = source["PermissionMode"];
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

