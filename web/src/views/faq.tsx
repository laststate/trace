import React from 'react'
import { Link } from 'react-router-dom'
import { BrandLogo } from '../Loading'
import './styles.css'

const faqs = [
  {
    q: "What is LastState?",
    a: "LastState is an open-source observability platform for embedded firmware and hardware fleets. It captures crash state across device reboots, collects it from any transport, and correlates issues across your fleet.",
  },
  {
    q: "What devices are supported?",
    a: "Latch supports Cortex-M (STM32, nRF, etc.), RISC-V (RV32/RV64), Xtensa (ESP32), and Linux signal capture. We maintain a hardware compatibility matrix with board-specific qualification reports.",
  },
  {
    q: "Can I use LastState for free?",
    a: "Yes. The Local plan is free for self-hosting, with unlimited devices, events, and retention. Paid plans add qualified hardware support, service levels, and other listed benefits. See the current pricing page for the offer and terms.",
  },
  {
    q: "Can I self-host everything?",
    a: "Absolutely. LastState is designed for self-hosted deployment. Run the full stack with `docker compose up` or use the `laststate` CLI. Your data stays on your infrastructure.",
  },
  {
    q: "What is the LEP protocol?",
    a: "LEP (LastState Event Protocol) v2 is the current frozen wire contract; v1 remains supported for compatibility. It defines transport-independent firmware diagnostic envelopes, identity, integrity, and framing.",
  },
  {
    q: "How does crash capture work?",
    a: "Latch captures a minimal snapshot of CPU context, breadcrumbs, metrics, and stack traces in retained RAM before a hard fault. After reboot, the snapshot is promoted to a durable spool and delivered via your preferred transport.",
  },
  {
    q: "Do I need a Relay?",
    a: "Relay is optional but recommended for production. It provides offline-first collection, LEP validation, local analysis, and reliable delivery to Trace. For simple setups, Latch can send directly via UART or TCP.",
  },
  {
    q: "What about security and compliance?",
    a: "Latch supports authenticated encryption and key-provider contracts, but its cryptography has not had an independent audit. LastState does not claim SOC 2 certification. Check the security and hardware qualification documentation before production use; support and service commitments depend on the selected plan.",
  },
  {
    q: "How do I contribute?",
    a: "We welcome contributions! Start with `good first issue` labels on GitHub. Integration experience, board qualification reports, documentation fixes, and test vectors are all valuable. See CONTRIBUTING.md in each repo.",
  },
  {
    q: "What about support?",
    a: "The Local plan includes community support. Paid plans list their support and service commitments on the current pricing page. There is no Team plan in the current offer.",
  },
]

export default function FAQPage() {
  return (
    <div className="faq-page">
      <Link to="/overview" className="page-back">← Dashboard</Link>
      <header className="faq-header">
        <div className="brand">
          <BrandLogo size={32} />
          <span>LastState</span>
        </div>
        <h1>Frequently Asked Questions</h1>
      </header>

      <div className="faq-list">
        {faqs.map((faq, i) => (
          <details key={i} className="faq-item">
            <summary>{faq.q}</summary>
            <p>{faq.a}</p>
          </details>
        ))}
      </div>

      <section className="faq-cta">
        <h2>Still have questions?</h2>
        <p className="meta">
          Open a discussion on GitHub or contact us at hello@laststate.dev
        </p>
        <a href="https://github.com/laststate/trace/discussions" className="btn primary">
          GitHub Discussions
        </a>
      </section>

      <footer className="faq-footer">
        <p className="meta">© 2026 LastState. Apache 2.0 & AGPL-3.0.</p>
      </footer>
    </div>
  )
}
