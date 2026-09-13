import module java.base;
import module java.net.http;
import module jdk.httpserver;

public class HttpTraffic {
    public static void start() {
        try {
            HttpServer server = HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 64);
            server.createContext("/orders/stats", exchange -> {
                byte[] body = ("{\"processed\":" + Orders.PROCESSED.sum() + ",\"queued\":" + Orders.QUEUE.size() + "}").getBytes(StandardCharsets.UTF_8);
                exchange.getResponseHeaders().add("Content-Type", "application/json");
                exchange.sendResponseHeaders(200, body.length);
                try (var out = exchange.getResponseBody()) {
                    out.write(body);
                }
            });
            server.setExecutor(Executors.newVirtualThreadPerTaskExecutor());
            server.start();
            URI uri = URI.create("http://127.0.0.1:" + server.getAddress().getPort() + "/orders/stats");
            Thread.ofPlatform().name("stats-poller").daemon(true).start(() -> poll(uri));
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }

    static void poll(URI uri) {
        try (HttpClient client = HttpClient.newHttpClient()) {
            while (true) {
                client.send(HttpRequest.newBuilder(uri).build(), HttpResponse.BodyHandlers.ofString());
                Thread.sleep(250);
            }
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
