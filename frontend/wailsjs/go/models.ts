export namespace config {
	
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
	    password_encrypted?: string;
	
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
	        this.password_encrypted = source["password_encrypted"];
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

export namespace cost {
	
	export class VPSInstance {
	    id: string;
	    vpsName: string;
	    host?: string;
	    cpu: number;
	    memory_gb: number;
	    disk_gb: number;
	    bandwidth_mbps: number;
	    traffic_gb: number;
	    ipv4Count: number;
	    price: number;
	    currency: string;
	    billingCycle: string;
	    purchaseDate: string;
	    nextRenewal: string;
	    manualRenewal: boolean;
	    providerName?: string;
	    providerURL?: string;
	    planName?: string;
	    os?: string;
	    profileId?: string;
	    notes?: string;
	
	    static createFrom(source: any = {}) {
	        return new VPSInstance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.vpsName = source["vpsName"];
	        this.host = source["host"];
	        this.cpu = source["cpu"];
	        this.memory_gb = source["memory_gb"];
	        this.disk_gb = source["disk_gb"];
	        this.bandwidth_mbps = source["bandwidth_mbps"];
	        this.traffic_gb = source["traffic_gb"];
	        this.ipv4Count = source["ipv4Count"];
	        this.price = source["price"];
	        this.currency = source["currency"];
	        this.billingCycle = source["billingCycle"];
	        this.purchaseDate = source["purchaseDate"];
	        this.nextRenewal = source["nextRenewal"];
	        this.manualRenewal = source["manualRenewal"];
	        this.providerName = source["providerName"];
	        this.providerURL = source["providerURL"];
	        this.planName = source["planName"];
	        this.os = source["os"];
	        this.profileId = source["profileId"];
	        this.notes = source["notes"];
	    }
	}

}

