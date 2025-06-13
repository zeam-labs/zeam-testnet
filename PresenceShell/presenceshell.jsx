import { useEffect, useState } from 'react';

const ZEAM_HOST = "http://IP_ADDRESS:8080";

export default function PresenceShell() {
    const [input, setInput] = useState('');
    const [output, setOutput] = useState([]);
    const [presence, setPresence] = useState({ id: '', name: '' });
    const [signedIn, setSignedIn] = useState(false);

    // Output polling
    const fetchOutput = async () => {
        try {
            const res = await fetch(`${ZEAM_HOST}/output`);
            const data = await res.json();

            console.log("Output from server:", data); // Confirm structure

            if (Array.isArray(data)) {
                setOutput((prev) => [...prev, ...data]);
            } else {
                console.warn("Invalid output format:", data);
            }
        } catch (err) {
            console.error("Output fetch failed:", err);
        }
    };

    // Submit reflection
    const handleSubmit = async () => {
        await fetch(`${ZEAM_HOST}/input`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                source: 'presence.ui',
                content: input,
                timestamp: new Date().toISOString(),
            }),
        });
        setInput('');
        fetchOutput();
    };

    // Attestation-style sign-in
    const signIn = async () => {
        const sbm = "zeam-sbm:test-" + Math.floor(Math.random() * 10000);
        const name = "Presence-" + sbm.slice(-4);

        const res = await fetch(`${ZEAM_HOST}/presence`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ type: "presence", id: sbm, name }),
        });

        const data = await res.json();
        localStorage.setItem('zeam-presence', JSON.stringify(data));
        setPresence(data);
        setSignedIn(true);
    };

    // Civic daemons
    const startCivicCompute = () => {
        setInterval(async () => {
            await fetch(`${ZEAM_HOST}/compute`);
        }, 10000);
    };

    const startCivicStorage = () => {
        setInterval(async () => {
            await fetch(`${ZEAM_HOST}/storage`);
        }, 15000);
    };

    // Polling loop
    useEffect(() => {
        if (!signedIn) return;
        const out = setInterval(fetchOutput, 3000);
        startCivicCompute();
        startCivicStorage();
        return () => clearInterval(out);
    }, [signedIn]);

    return (
        <main className="min-h-screen bg-gray-950 text-white p-6 space-y-8">
            <section>
                <h1 className="text-2xl font-bold">ZEAM Presence Shell</h1>
                <p className="text-sm text-gray-400">Biometric Attestation Portal</p>
            </section>

            {!signedIn && (
                <section>
                    <button
                        onClick={signIn}
                        className="bg-blue-600 hover:bg-blue-700 px-6 py-3 rounded text-lg"
                    >
                        Sign In (Simulated Attestation)
                    </button>
                </section>
            )}

            {signedIn && (
                <>
                    <section className="space-y-4">
                        <h2 className="text-lg font-semibold">Reflect</h2>
                        <textarea
                            value={input}
                            onChange={(e) => setInput(e.target.value)}
                            className="w-full p-2 bg-gray-800 rounded"
                            placeholder="Type your reflection here..."
                        />
                        <button
                            onClick={handleSubmit}
                            className="bg-green-600 hover:bg-green-700 px-4 py-2 rounded"
                        >
                            Submit
                        </button>
                    </section>

                    <section className="space-y-2">
                        <h2 className="text-lg font-semibold">Live Output</h2>
                        <div className="bg-gray-800 rounded p-3 h-48 overflow-y-auto">
                            {output.map((msg, idx) => (
                                <div key={idx} className="text-sm text-green-300">{msg}</div>
                            ))}
                        </div>
                    </section>
                </>
            )}
        </main>
    );
}
