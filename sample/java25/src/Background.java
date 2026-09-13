import module java.base;

public class Background {
    static final Object EVENTS = new Object();
    static final Map<String, Long> CACHE = new ConcurrentHashMap<>();

    public static void start() {
        Thread.ofPlatform().name("cpu-hasher").daemon(true).start(Background::burn);
        Thread.ofPlatform().name("event-listener").daemon(true).start(Background::listen);
        var scheduler = Executors.newSingleThreadScheduledExecutor(Thread.ofPlatform().name("cache-refresher").daemon(true).factory());
        scheduler.scheduleAtFixedRate(Background::refresh, 0, 1, TimeUnit.SECONDS);
        var virtual = Executors.newVirtualThreadPerTaskExecutor();
        for (int i = 0; i < 500; i++) {
            int n = i;
            virtual.submit(() -> {
                while (true) {
                    Orders.pause(500 + n);
                    CACHE.merge("virtual-" + (n % 10), 1L, Long::sum);
                }
            });
        }
    }

    static void burn() {
        while (true) {
            long until = System.nanoTime() + 100_000_000L;
            while (System.nanoTime() < until) {
                Hashing.sha256("cpu", 500);
            }
            Orders.pause(150);
        }
    }

    static void refresh() {
        CACHE.put("orders.processed", Orders.PROCESSED.sum());
        CACHE.put("refreshed.at", System.currentTimeMillis());
        synchronized (EVENTS) {
            EVENTS.notifyAll();
        }
    }

    static void listen() {
        long seen = 0;
        while (true) {
            synchronized (EVENTS) {
                try {
                    EVENTS.wait();
                } catch (InterruptedException e) {
                    return;
                }
            }
            seen++;
            if (seen % 30 == 0) {
                IO.println("events " + seen + " cache " + CACHE.size() + " processed " + Orders.PROCESSED.sum());
            }
        }
    }
}
