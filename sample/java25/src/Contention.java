public class Contention {
    static final Object REPORT = new Object();

    public static void start() {
        Thread.ofPlatform().name("report-exporter").daemon(true).start(Contention::export);
        for (int i = 1; i <= 3; i++) {
            Thread.ofPlatform().name("report-reader-" + i).daemon(true).start(Contention::read);
        }
    }

    static void export() {
        while (true) {
            synchronized (REPORT) {
                Hashing.sha256("report", 50_000);
                Orders.pause(1_500);
            }
            Orders.pause(50);
        }
    }

    static void read() {
        while (true) {
            synchronized (REPORT) {
                Orders.pause(20);
            }
        }
    }
}
