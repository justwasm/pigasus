//DecompilePriority.java - Ghidra postScript to decompile priority functions to .c files
//Reads /Users/gear/GitHub/openagy/out/local-macos/priority-funcs.txt, decompiles each function
//using DecompInterface, writes <addr>_<safename>.c to /Users/gear/GitHub/openagy/out/local-macos/dec/.
//
//Format of priority-funcs.txt: 0xADDR\tfunction\tNAME  (tab-separated)
//
//Usage:
//  $GHIDRA_INSTALL_DIR/support/analyzeHeadless /Users/gear/GitHub/openagy/out/local-macos/proj proj \
//    -process -noanalysis -postScript /workspace/agy/out/decompile/DecompilePriority.java
//
//The script decompiles each priority function with a 60-second timeout, writing
//the C pseudocode to a per-function file. Each line of the priority file is
//processed independently so partial progress survives a script crash.

import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.app.decompiler.DecompiledFunction;
import ghidra.program.model.listing.Function;
import ghidra.program.model.listing.FunctionManager;
import ghidra.program.model.address.Address;
import ghidra.program.model.address.AddressFactory;
import java.io.*;

public class DecompilePriority extends GhidraScript {

    private static final String PRIORITY_FILE = "/Users/gear/GitHub/openagy/out/local-macos/priority-funcs.txt";
    private static final String OUT_DIR = "/Users/gear/GitHub/openagy/out/local-macos/dec";
    private static final int DECOMPILE_TIMEOUT_SEC = 60;
    private static final int MAX_SKIP = 0; // Process all entries

    private static String sanitize(String name) {
        // Convert a Go function name to a filesystem-safe stem.
        // Strip the package path prefix, keep only the part after the last '.',
        // then replace non-alphanumeric characters with '_'.
        int lastDot = name.lastIndexOf('.');
        String stem = lastDot >= 0 ? name.substring(lastDot + 1) : name;
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < stem.length(); i++) {
            char c = stem.charAt(i);
            if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
                sb.append(c);
            } else {
                sb.append('_');
            }
        }
        // Truncate to 80 chars to avoid filesystem limits
        if (sb.length() > 80) sb.setLength(80);
        return sb.toString();
    }

    private static String addrHex(String addrTok) {
        // addrTok looks like "0x61a3460"
        if (addrTok.startsWith("0x") || addrTok.startsWith("0X")) {
            return addrTok.substring(2);
        }
        return addrTok;
    }

    public void run() throws Exception {
        File outDir = new File(OUT_DIR);
        if (!outDir.exists()) {
            outDir.mkdirs();
        }
        File progressFile = new File(OUT_DIR, "_progress.txt");
        File errorFile = new File(OUT_DIR, "_errors.txt");
        File summaryFile = new File(OUT_DIR, "_summary.txt");

        int totalProcessed = 0;
        int totalDecompiled = 0;
        int totalSkipped = 0;
        int totalErrors = 0;

        PrintWriter errPw = new PrintWriter(new FileWriter(errorFile, true));
        PrintWriter sumPw = new PrintWriter(new FileWriter(summaryFile));
        long startMs = System.currentTimeMillis();

        // Open the decompiler
        println("DecompilePriority: opening decompiler...");
        DecompInterface decomp = new DecompInterface();
        decomp.openProgram(currentProgram);
        // DecompInterface.setOptionsMemoryMode doesn't exist; default options are fine.
        println("DecompilePriority: decompiler ready.");

        FunctionManager funcMgr = currentProgram.getFunctionManager();
        AddressFactory af = currentProgram.getAddressFactory();

        BufferedReader br = new BufferedReader(new FileReader(PRIORITY_FILE));
        String line;
        while ((line = br.readLine()) != null && totalProcessed < 100000) {
            line = line.trim();
            if (line.isEmpty() || line.startsWith("#")) continue;
            String[] parts = line.split("\t");
            if (parts.length < 3) {
                errPw.println("SKIP\tmalformed: " + line);
                totalSkipped++;
                continue;
            }
            String addrTok = parts[0];
            String kind = parts[1]; // 'function', 'label', etc.
            String name = parts[2];

            if (!kind.equals("function")) {
                errPw.println("SKIP\tnot a function: " + line);
                totalSkipped++;
                continue;
            }

            String hex = addrHex(addrTok);
            Address addr;
            try {
                addr = af.getAddress("0x" + hex);
            } catch (Exception e) {
                errPw.println("SKIP\tbad address: " + addrTok + " (" + e.getMessage() + ")");
                totalSkipped++;
                continue;
            }

            Function func = funcMgr.getFunctionAt(addr);
            if (func == null) {
                errPw.println("SKIP\tno function at " + addrTok + " (" + name + ")");
                totalSkipped++;
                continue;
            }

            String safe = sanitize(name);
            String outName = String.format("%s_%s.c", hex, safe);
            File outFile = new File(OUT_DIR, outName);
            // Skip if already done
            if (outFile.exists() && outFile.length() > 100) {
                totalProcessed++;
                totalDecompiled++;
                continue;
            }

            try {
                DecompileResults res = decomp.decompileFunction(func, DECOMPILE_TIMEOUT_SEC, monitor);
                totalProcessed++;
                if (res != null && res.decompileCompleted()) {
                    DecompiledFunction df = res.getDecompiledFunction();
                    if (df != null) {
                        String code = df.getC();
                        PrintWriter pw = new PrintWriter(new FileWriter(outFile));
                        pw.println("// Source: 0x" + hex + "  " + name);
                        pw.println("// Decompiled at: " + new java.util.Date());
                        pw.println("// Signature: " + func.getSignature().toString());
                        pw.println();
                        pw.print(code);
                        pw.close();
                        totalDecompiled++;
                        // Log progress every 50 funcs
                        if (totalProcessed % 50 == 0) {
                            long elapsed = (System.currentTimeMillis() - startMs) / 1000;
                            println(String.format("DecompilePriority: %d processed, %d ok, %d err, %d skip (%ds elapsed)",
                                totalProcessed, totalDecompiled, totalErrors, totalSkipped, elapsed));
                        }
                    } else {
                        errPw.println("ERR\tnull decompiled function: 0x" + hex + " " + name);
                        totalErrors++;
                    }
                } else {
                    String errMsg = (res != null && res.getErrorMessage() != null) ? res.getErrorMessage() : "decompile did not complete";
                    errPw.println("ERR\t" + errMsg + ": 0x" + hex + " " + name);
                    totalErrors++;
                }
            } catch (Exception e) {
                errPw.println("EXC\t" + e.getClass().getSimpleName() + ": " + e.getMessage() + ": 0x" + hex + " " + name);
                totalErrors++;
                if (monitor.isCancelled()) {
                    errPw.println("ABORT\tmonitor cancelled");
                    break;
                }
            }
        }
        br.close();

        long elapsedSec = (System.currentTimeMillis() - startMs) / 1000;
        sumPw.println("# DecompilePriority summary");
        sumPw.println("# Started:  " + new java.util.Date(startMs));
        sumPw.println("# Ended:    " + new java.util.Date());
        sumPw.println("# Elapsed:  " + elapsedSec + " sec (" + (elapsedSec / 60) + " min)");
        sumPw.println("# Total processed: " + totalProcessed);
        sumPw.println("# Total decompiled OK: " + totalDecompiled);
        sumPw.println("# Total errors: " + totalErrors);
        sumPw.println("# Total skipped: " + totalSkipped);
        sumPw.println("# Output dir: " + OUT_DIR);
        sumPw.close();
        errPw.close();

        println(String.format("DecompilePriority DONE: %d processed, %d ok, %d err, %d skip in %d sec",
            totalProcessed, totalDecompiled, totalErrors, totalSkipped, elapsedSec));
    }
}
