import com.fr.stable.Ascii85InputStream;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.*;

public class Ascii85Oracle {
    // Ground-truth oracle: decodes payloads with FineReport's own
    // com.fr.stable.Ascii85InputStream and prints the raw bytes as hex.
    // Usage:
    //   javac -cp <dir-with-com/fr/stable> Ascii85Oracle.java
    //   java -cp . Ascii85Oracle < payloads.txt
    public static void main(String[] args) throws Exception {
        // 从 stdin 读 payload（每行一条），输出 a85 输出的 hex
        BufferedReader br = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8));
        PrintStream out = new PrintStream(System.out, true, "UTF-8");
        String line;
        while ((line = br.readLine()) != null) {
            if (line.isEmpty()) continue;
            byte[] ascii = line.getBytes("US-ASCII");
            Ascii85InputStream in = new Ascii85InputStream(new ByteArrayInputStream(ascii));
            ByteArrayOutputStream bo = new ByteArrayOutputStream();
            int b;
            try { while ((b = in.read()) != -1) bo.write(b); } catch (Exception e) { out.println("ERR"); continue; }
            StringBuilder sb = new StringBuilder();
            for (byte x : bo.toByteArray()) sb.append(String.format("%02x", x));
            out.println(sb.toString());
        }
    }
}
