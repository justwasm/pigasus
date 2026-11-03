// FindAddrRefs.java — print the functions that reference each address
// given as a script argument.
//
//   analyzeHeadless <proj> agy -process -noanalysis -max-cpu 4 \
//     -scriptPath <dir> -postScript FindAddrRefs.java 0x1033ebfd7 0x103392681
//
// Ghidra's reference table is authoritative: the compiler materialises a
// string constant's address in several ways (a static (ptr, len) pair in
// __rodata, a MOVZ/MOVK sequence, an ADRP+ADD pair), and only the
// reference manager sees all of them.
//
// The addresses are the *string data*, not the (ptr, len) header: a
// reference to the header would not show up here.
import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.listing.Function;
import ghidra.program.model.symbol.Reference;

public class FindAddrRefs extends GhidraScript {
    @Override
    public void run() throws Exception {
        for (String a : getScriptArgs()) {
            String t = a.trim();
            if (t.isEmpty()) {
                continue;
            }
            Address addr = toAddr(Long.decode(t));
            println("=== " + t + "  refs:");
            Reference[] refs = getReferencesTo(addr);
            if (refs.length == 0) {
                println("  (none)");
            }
            for (Reference ref : refs) {
                Address from = ref.getFromAddress();
                Function func = currentProgram.getFunctionManager().getFunctionContaining(from);
                println(String.format("  from %s in %s", from,
                        func != null ? func.getName() : "<no-func>"));
            }
        }
    }
}
