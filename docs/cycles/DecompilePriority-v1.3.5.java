//DecompilePriority-v1.3.5.java - Ghidra postScript to decompile priority functions
//Specific to 1.3.5 binary, with paths parameterized via script args.
//
//Usage:
//  $GHIDRA_INSTALL_DIR/support/analyzeHeadless /tmp/ghidra-1.3.5-project agy-1.3.5 \
//    -import /workspace/agy/install/antigravity-v1.3.5 \
//    -Xmx1500M \
//    -preScript ReaGhidraApplySeeds.java <seeds.tsv> \
//    -postScript DecompilePriority-v1.3.5.java <priority.txt> <out_dir>
//
//Format of priority file: 0xADDR\tfunction\tNAME  (tab-separated)
//Each line is decompiled independently so partial progress survives a crash.

import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.app.decompiler.DecompiledFunction;
import ghidra.program.model.listing.Function;
import ghidra.program.model.listing.FunctionManager;
import ghidra.program.model.address.Address;
import ghidra.program.model.address.AddressFactory;
import java.io.*;

public class DecompilePriorityV135 extends GhidraScript {

    private static final int DECOMPILE_TIMEOUT_SEC = 5;

    private String priorityFile;
    private String outDir;

    private static String sanitize(String name) {
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
        if (sb.length() > 80) sb.setLength(80);
        return sb.toString();
    }

    private static String addrHex(String addrTok) {
        if (addrTok.startsWith("0x") || addrTok.startsWith("0X")) {
            return addrTok.substring(2);
        }
        return addrTok;
    }

    public void run() throws Exception {
        String[] args = getScriptArgs();
        if (args.length < 2) {
            println("DecompilePriority: usage <priority_file> <out_dir>");
            return;
        }
        priorityFile = args[0];
        outDir = args[1];

        File outDirFile = new File(outDir);
        if (!outDirFile.exists()) {
            outDirFile.mkdirs();
        }
        File progressFile = new File(outDir, "_progress.txt");
        File errorFile = new File(outDir, "_errors.txt");
        File summaryFile = new File(outDir, "_summary.txt");

        int totalProcessed = 0;
        int totalDecompiled = 0;
        int totalSkipped = 0;
        int totalErrors = 0;

        PrintWriter errPw = new PrintWriter(new FileWriter(errorFile, true));
        PrintWriter sumPw = new PrintWriter(new FileWriter(summaryFile));
        long startMs = System.currentTimeMillis();

        println("DecompilePriority: opening decompiler...");
        DecompInterface decomp = new DecompInterface();
        decomp.openProgram(currentProgram);
        println("DecompilePriority: decompiler ready.");
        println("DecompilePriority: priority file = " + priorityFile);
        println("DecompilePriority: out dir = " + outDir);

        FunctionManager funcMgr = currentProgram.getFunctionManager();
        AddressFactory af = currentProgram.getAddressFactory();

        BufferedReader br = new BufferedReader(new FileReader(priorityFile));
        String line;
        int lineNum = 0;
        while ((line = br.readLine()) != null && totalProcessed < 100000) {
            lineNum++;
            line = line.trim();
            if (line.isEmpty() || line.startsWith("#")) continue;
            String[] parts = line.split("\t");
            if (parts.length < 3) {
                errPw.println("SKIP\tmalformed line " + lineNum + ": " + line);
                totalSkipped++;
                continue;
            }
            String addrTok = parts[0];
            String kind = parts[1];
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
            File outFile = new File(outDir, outName);
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
        sumPw.println("# DecompilePriority-v1.3.5 summary");
        sumPw.println("# Started:  " + new java.util.Date(startMs));
        sumPw.println("# Ended:    " + new java.util.Date());
        sumPw.println("# Elapsed:  " + elapsedSec + " sec (" + (elapsedSec / 60) + " min)");
        sumPw.println("# Total processed: " + totalProcessed);
        sumPw.println("# Total decompiled OK: " + totalDecompiled);
        sumPw.println("# Total errors: " + totalErrors);
        sumPw.println("# Total skipped: " + totalSkipped);
        sumPw.println("# Output dir: " + outDir);
        sumPw.close();
        errPw.close();

        println(String.format("DecompilePriority DONE: %d processed, %d ok, %d err, %d skip in %d sec",
            totalProcessed, totalDecompiled, totalErrors, totalSkipped, elapsedSec));
    }
}
