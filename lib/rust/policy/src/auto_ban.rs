struct AutobanTracker {}

impl AutobanTracker {
    pub fn new() -> Self {
        Self {}
    }

    pub fn track(&mut self, ip: &IpAddr) {
        // TODO: implement auto-ban tracking
    }

    pub fn is_banned(&self, ip: &IpAddr) -> bool {
        // TODO: implement auto-ban check
        false
    }
}
