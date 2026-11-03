// DisasmRange.java — disassemble a range of instructions.
//
//   analyzeHeadless <proj> agy -process -noanalysis -max-cpu 4 \
//     -scriptPath <dir> -postScript DisasmRange.java <start> [count]
//
// The start address is a hex address (0x…); the count is the number of
// instructions to print (20 by default). The script prints the address,
// the mnemonic and the operands of every instruction it can read.
//
// The decompiler's model of a Go function's stack slots is not always
// faithful — the `[]any` a fmt.Sprintf takes is the usual offender —
// so a row's arguments sometimes have to be read out of the
// instructions themselves: at a runtime.convTstring call the two
// registers hold the string's pointer and its length, and tracing them
// back to the slot they were loaded from says which slot each argument
// of the format came from. Cycle 319 read the /tasks row's six
// arguments this way.
import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.listing.Instruction;

public class DisasmRange extends GhidraScript {
    @Override
    public void run() throws Exception {
        String[] args = getScriptArgs();
        if (args.length < 1) {
            println("usage: DisasmRange <start> [count]");
            return;
        }
        Address addr = toAddr(Long.parseLong(args[0].trim().replace("0x", ""), 16));
        int count = 20;
        if (args.length > 1) {
            count = Integer.parseInt(args[1].trim());
        }
        for (int i = 0; i < count; i++) {
            Instruction ins = getInstructionAt(addr);
            if (ins == null) break;
            println(String.format("0x%x: %s", addr.getOffset(), ins));
            addr = addr.add(ins.getLength());
        }
    }
}
