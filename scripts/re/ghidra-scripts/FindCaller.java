import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.symbol.Reference;

public class FindCaller extends GhidraScript {
    @Override
    public void run() throws Exception {
        Address addr = toAddr(0x102388f20L);
        Reference[] refs = getReferencesTo(addr);
        for (Reference r : refs) {
            println(String.format("Xref from 0x%x to 0x102388f20", r.getFromAddress().getOffset()));
        }
    }
}
