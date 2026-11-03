import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.listing.Instruction;

public class DisasmAt extends GhidraScript {
    @Override
    public void run() throws Exception {
        Address addr = toAddr(0x102389a30L);
        for (int i = 0; i < 20; i++) {
            Instruction ins = getInstructionAt(addr);
            if (ins == null) break;
            println(String.format("0x%x: %s", addr.getOffset(), ins));
            addr = addr.add(ins.getLength());
        }
    }
}
