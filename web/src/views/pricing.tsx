import React from 'react'
import { Link } from 'react-router-dom'
import { BrandLogo } from '../Loading'
import './styles.css'

export default function PricingPage() {
  const plans = [
    {
      id: 'local', name: 'Local', price: '$0', period: 'forever · self-host',
      devices: 'Unlimited', events: 'Unlimited', retention: 'Unlimited',
      features: ['Symbolication + issue tracking', 'Community support', 'No card or quotas'],
      highlight: false,
    },
    {
      id: 'pilot', name: 'Pilot', price: '$499', period: '/mo · 1 qualified board',
      devices: '100', events: 'Plan limits apply', retention: '90 days',
      features: ['Signed updates', 'Crash-reproduced SLO', 'Direct core-team support'],
      highlight: true,
    },
    {
      id: 'fleet', name: 'Fleet', price: '$1,999', period: '/mo · 3 qualified boards',
      devices: '1,000', events: 'Plan limits apply', retention: '1 year',
      features: ['ELF/DWARF symbolication', 'Postmortem generation (customer LLM key required)', '99.5% ingest SLA'],
      highlight: false,
    },
    {
      id: 'enterprise', name: 'Enterprise', price: '$7,999', period: '/mo · signed matrix',
      devices: 'Unlimited', events: 'Plan limits apply', retention: 'Unlimited',
      features: ['99.9% SLA + credits', 'Commercial Trace license', 'FAE + on-site workshop'],
      highlight: false,
    },
  ]

  return (
    <div className="pricing-page">
      <Link to="/overview" className="page-back">← Dashboard</Link>
      {/* Header */}
      <header className="pricing-header">
        <div className="brand">
          <BrandLogo size={32} />
          <span>LastState</span>
        </div>
        <h1>Pricing</h1>
        <p className="meta">Self-host for free. Paid plans add qualified hardware support and service commitments.</p>
      </header>

      {/* Plans */}
      <div className="pricing-grid">
        {plans.map(plan => (
          <div
            key={plan.id}
            className={`pricing-card ${plan.highlight ? 'pricing-highlight' : ''}`}
          >
            {plan.highlight && <div className="pricing-badge">Most popular</div>}
            <h2>{plan.name}</h2>
            <div className="pricing-price">
              {plan.price}<span className="pricing-period">{plan.period}</span>
            </div>
            <ul className="pricing-features">
              <li><strong>{plan.devices}</strong> devices</li>
              <li><strong>{plan.events}</strong> events</li>
              <li><strong>{plan.retention}</strong> retention</li>
            </ul>
            <hr />
            <ul className="pricing-includes">
              {plan.features.map(f => (
                <li key={f}>✓ {f}</li>
              ))}
            </ul>
            <a href={plan.id === 'local' ? 'https://laststate.io/docs' : 'https://laststate.io/design-partner'} className="btn primary" style={{ width: '100%', textAlign: 'center' }}>
              {plan.id === 'local' ? 'Get started' : 'Talk to the team'}
            </a>
          </div>
        ))}
      </div>

      {/* FAQ */}
      <section className="pricing-faq">
        <h2>Frequently asked questions</h2>
        <div className="faq-list">
          <details>
            <summary>What happens when I exceed my limits?</summary>
            <p>Device and event limits are defined by each paid plan. Contact the team before rollout if your fleet may exceed its plan limits.</p>
          </details>
          <details>
            <summary>Can I cancel a subscription?</summary>
            <p>Cancellation and access timing follow the subscription terms. Contact the team for current billing details.</p>
          </details>
          <details>
            <summary>Is there a free trial?</summary>
            <p>Local self-hosting is free. Paid hardware qualification starts with a 30-day, one-board POC; see the current terms before purchasing.</p>
          </details>
          <details>
            <summary>What payment methods do you accept?</summary>
            <p>Available payment methods depend on the configured checkout and your region. Contact the team for current payment options.</p>
          </details>
          <details>
            <summary>Can I self-host for free?</summary>
            <p>Yes. The Local plan is free self-hosted with unlimited devices, events, and retention. See the current terms for paid services.</p>
          </details>
          <details>
            <summary>Where can I see current plans and terms?</summary>
            <p>See <a href="https://laststate.io/pricing">LastState pricing</a> for current plan details and contact the team before relying on a service-level commitment.</p>
          </details>
        </div>
      </section>

      {/* CTA */}
      <section className="pricing-cta">
        <h2>Ready to get started?</h2>
        <p className="meta">Start with free self-hosting or discuss a qualified hardware pilot.</p>
        <div className="pricing-actions">
          <a href="/docs" className="btn primary">Read the docs</a>
          <a href="https://laststate.io/pricing" className="btn secondary">Current plans</a>
        </div>
      </section>

      {/* Footer */}
      <footer className="pricing-footer">
        <p className="meta">© 2026 LastState. Apache 2.0 & AGPL-3.0.</p>
      </footer>
    </div>
  )
}
