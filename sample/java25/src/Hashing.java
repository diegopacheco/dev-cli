import module java.base;

public class Hashing {
    public static byte[] sha256(String text, int rounds) {
        try {
            var digest = MessageDigest.getInstance("SHA-256");
            byte[] value = text.getBytes(StandardCharsets.UTF_8);
            for (int i = 0; i < rounds; i++) {
                value = digest.digest(value);
            }
            return value;
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException(e);
        }
    }
}
