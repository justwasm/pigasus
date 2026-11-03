import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;

public class ReadString extends GhidraScript {
    @Override
    public void run() throws Exception {
        Address addr = toAddr(0x102ea25c8L);
        byte[] b = new byte[0x1d];
        for (int i = 0; i < 0x1d; i++) {
            b[i] = getByte(addr.add(i));
        }
        println("String at 0x102ea25c8: " + new String(b));
    }
}
