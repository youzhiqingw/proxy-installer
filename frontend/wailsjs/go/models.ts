export namespace main {
	
	export class DeployConfig {
	    profileId: string;
	    nodeName: string;
	    selected: string[];
	    ports: Record<string, number>;
	    publicPorts: Record<string, number>;
	    webPort: number;
	    publicWebPort: number;
	    token: string;
	    rule: string;
	    sni: string;
	
	    static createFrom(source: any = {}) {
	        return new DeployConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.nodeName = source["nodeName"];
	        this.selected = source["selected"];
	        this.ports = source["ports"];
	        this.publicPorts = source["publicPorts"];
	        this.webPort = source["webPort"];
	        this.publicWebPort = source["publicWebPort"];
	        this.token = source["token"];
	        this.rule = source["rule"];
	        this.sni = source["sni"];
	    }
	}
	export class SSHProfile {
	    id: string;
	    name: string;
	    host: string;
	    user: string;
	    username: string;
	    port: number;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.user = source["user"];
	        this.username = source["username"];
	        this.port = source["port"];
	        this.password = source["password"];
	    }
	}
	export class AppState {
	    profiles: SSHProfile[];
	    deployConfig: DeployConfig;
	    activeClient: string;
	    updatedAt: string;
	    extra?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new AppState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profiles = this.convertValues(source["profiles"], SSHProfile);
	        this.deployConfig = this.convertValues(source["deployConfig"], DeployConfig);
	        this.activeClient = source["activeClient"];
	        this.updatedAt = source["updatedAt"];
	        this.extra = source["extra"];
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

