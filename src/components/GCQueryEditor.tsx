import defaults from "lodash/defaults";
import React, { ChangeEvent, PureComponent } from "react";
import { LegacyForms, Select, Spinner } from "@grafana/ui";
import { QueryEditorProps, SelectableValue } from "@grafana/data";
import { DataSource } from "../datasource";
import {
  GCDataSourceOptions,
  GCProduct,
  GCGranularity,
  GCGrouping,
  GCQuery,
  GCMetric,
  GC_PRODUCTS,
  GCDNSGranularity,
  GCDNSRecordType,
  GCVariable,
} from "../types";
import { GCSelectMetric } from "./GCSelectMetric";
import { GCSelectGranularity } from "./GCSelectGranularity";
import { GCSelectGrouping } from "./GCSelectGrouping";
import { GCInput } from "./GCInput";
import {
  defaultQuery,
  defaultDNSQuery,
  defaultFastEdgeQuery,
  defaultWAAPQuery,
} from "../defaults";

const { FormField } = LegacyForms;

const DNS_GRANULARITY_OPTS: Array<SelectableValue<string>> = Object.values(
  GCDNSGranularity
).map((g) => ({ value: g, label: g }));

const DNS_RECORD_TYPE_OPTS: Array<SelectableValue<string>> = Object.values(
  GCDNSRecordType
).map((r) => ({ value: r, label: r }));

const WAAP_METRIC_OPTS: Array<SelectableValue<string>> = [
  { label: "total_requests", value: "total_requests" },
  { label: "total_bytes", value: "total_bytes" },
];

const WAAP_GRANULARITY_OPTS: Array<SelectableValue<string>> = [
  { label: "1h", value: "1h" },
  { label: "1d", value: "1d" },
];

const FASTEDGE_METRIC_OPTS: Array<SelectableValue<string>> = [
  { label: "avg", value: "avg" },
  { label: "min", value: "min" },
  { label: "max", value: "max" },
  { label: "median", value: "median" },
  { label: "perc75", value: "perc75" },
  { label: "perc90", value: "perc90" },
];

type Props = QueryEditorProps<DataSource, GCQuery, GCDataSourceOptions>;

type State = {
  dnsZones: SelectableValue<string>[];
  dnsZonesLoading: boolean;
  fastedgeApps: Array<SelectableValue<number>>;
  fastedgeAppsLoading: boolean;
};

export class GCQueryEditor extends PureComponent<Props, State> {
  state: State = {
    dnsZones: [],
    dnsZonesLoading: false,
    fastedgeApps: [],
    fastedgeAppsLoading: false,
  };

  componentDidMount() {
    this.persistProductDefault();
    const product = this.props.query?.product ?? "cdn";
    if (product === "dns") this.loadDnsZones();
    if (product === "fastedge") this.loadFastEdgeApps();
  }

  componentDidUpdate(prevProps: Props) {
    const prevProduct = prevProps.query?.product ?? "cdn";
    const product = this.props.query?.product ?? "cdn";
    if (prevProduct !== product) {
      this.persistProductDefault();
      if (product === "dns") this.loadDnsZones();
      if (product === "fastedge") this.loadFastEdgeApps();
    }
  }

  persistProductDefault = () => {
    const { query, onChange, onRunQuery } = this.props;
    const product = (query?.product ?? "cdn") as GCProduct;
    if (!query?.product) {
      onChange({ ...defaults(query, defaultQuery), product: "cdn" });
      onRunQuery?.();
      return;
    }
    if (product === "dns" && !query.dnsGranularity) {
      onChange({ ...query, ...defaultDNSQuery } as GCQuery);
      onRunQuery?.();
    } else if (product === "fastedge" && query.fastedgeMetric === undefined) {
      onChange({ ...query, ...defaultFastEdgeQuery } as GCQuery);
      onRunQuery?.();
    } else if (product === "waap" && !query.waapMetric) {
      onChange({ ...query, ...defaultWAAPQuery } as GCQuery);
      onRunQuery?.();
    }
  };

  loadDnsZones = async () => {
    this.setState({ dnsZonesLoading: true });
    try {
      const result = await this.props.datasource.metricFindQuery({
        selector: { value: GCVariable.Zone },
      });
      const zones: SelectableValue<string>[] = (result as Array<{ text: string }>).map(
        (z) => ({ value: z.text, label: z.text })
      );
      this.setState({
        dnsZones: [{ value: "all", label: "All Zones" }, ...zones],
        dnsZonesLoading: false,
      });
    } catch {
      this.setState({ dnsZones: [{ value: "all", label: "All Zones" }], dnsZonesLoading: false });
    }
  };

  loadFastEdgeApps = async () => {
    this.setState({ fastedgeAppsLoading: true });
    try {
      const raw = await this.props.datasource.getResource("fastedge/apps");
      const arr = Array.isArray(raw) ? raw : (raw as { apps?: { id: number; name?: string }[] })?.apps ?? [];
      const appOptions: Array<SelectableValue<number>> = [
        { label: "All", value: 0 },
        ...arr.map((app: { id: number; name?: string }) => ({
          label: app.name ?? `App ${app.id}`,
          value: app.id,
        })),
      ];
      this.setState({ fastedgeApps: appOptions, fastedgeAppsLoading: false });
    } catch {
      this.setState({ fastedgeApps: [{ label: "All", value: 0 }], fastedgeAppsLoading: false });
    }
  };

  onProductChange = (opt: SelectableValue<GCProduct>) => {
    const { onChange, query, onRunQuery } = this.props;
    const product = (opt?.value ?? "cdn") as GCProduct;
    const next: Partial<GCQuery> = { ...query, product };
    if (product === "cdn") Object.assign(next, defaultQuery);
    else if (product === "dns") Object.assign(next, defaultDNSQuery);
    else if (product === "fastedge") Object.assign(next, defaultFastEdgeQuery);
    else if (product === "waap") Object.assign(next, defaultWAAPQuery);
    onChange(next as GCQuery);
    onRunQuery?.();
  };

  // --- CDN handlers
  onMetricChange = (value: SelectableValue<GCMetric>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, metric: value });
    onRunQuery?.();
  };
  onGroupingChange = (value: Array<SelectableValue<GCGrouping>>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, grouping: value });
    onRunQuery?.();
  };
  onGranularityChange = (value: SelectableValue<GCGranularity>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, granularity: value });
    onRunQuery?.();
  };
  onVhostsChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, vhosts: e.target.value });
    onRunQuery?.();
  };
  onResourcesChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, resources: e.target.value });
    onRunQuery?.();
  };
  onClientsChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, clients: e.target.value });
    onRunQuery?.();
  };
  onRegionChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, regions: e.target.value });
    onRunQuery?.();
  };
  onCountryChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, countries: e.target.value });
    onRunQuery?.();
  };
  onLegendFormatChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, legendFormat: e.target.value });
    onRunQuery?.();
  };

  // --- DNS handlers
  onZoneChange = (opt: SelectableValue<string>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, zone: opt?.value });
    onRunQuery?.();
  };
  onDnsGranularityChange = (opt: SelectableValue<string>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, dnsGranularity: opt });
    onRunQuery?.();
  };
  onRecordTypeChange = (opt: SelectableValue<string>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, record_type: opt?.value });
    onRunQuery?.();
  };
  onDnsLegendFormatChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, dnsLegendFormat: e.target.value });
    onRunQuery?.();
  };

  // --- FastEdge handlers
  onAppChange = (opt: SelectableValue<number>) => {
    const { onChange, query, onRunQuery } = this.props;
    const name = opt?.value === 0 ? "All" : this.state.fastedgeApps.find((a) => a.value === opt?.value)?.label;
    onChange({ ...query, appId: opt?.value ?? 0, appName: name });
    onRunQuery?.();
  };
  onStepChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query, onRunQuery } = this.props;
    const step = parseInt(e.target.value, 10);
    onChange({ ...query, step: !Number.isNaN(step) && step > 0 ? step : 60 });
    onRunQuery?.();
  };
  onNetworkChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, network: e.target.value });
    onRunQuery?.();
  };
  onFastedgeMetricChange = (opt: SelectableValue<string>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, fastedgeMetric: opt?.value ?? "avg" });
    onRunQuery?.();
  };

  // --- WAAP handlers
  onWaapMetricChange = (opt: SelectableValue<string>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, waapMetric: opt?.value ?? "total_requests" });
    onRunQuery?.();
  };
  onWaapGranularityChange = (opt: SelectableValue<string>) => {
    const { onChange, query, onRunQuery } = this.props;
    onChange({ ...query, waapGranularity: (opt?.value as "1h" | "1d") ?? "1h" });
    onRunQuery?.();
  };
  onWaapLegendFormatChange = (e: ChangeEvent<HTMLInputElement>) => {
    const { onChange, query } = this.props;
    onChange({ ...query, waapLegendFormat: e.target.value });
  };

  render() {
    const query = defaults(this.props.query, defaultQuery) as GCQuery;
    const product = (query.product ?? "cdn") as GCProduct;
    const productOption = GC_PRODUCTS.find((p) => p.value === product) ?? GC_PRODUCTS[0];

    return (
      <div style={{ display: "flex", gap: "24px", alignItems: "flex-start" }}>
        <div className="gf-form-group" style={{ minWidth: "240px", maxWidth: "240px" }}>
          <label className="gf-form-group-label">Edge Network</label>
          <FormField
            label="Service"
            labelWidth={6}
            tooltip="Select Gcore service (CDN, DNS, FastEdge, WAAP)"
            inputEl={
              <Select<GCProduct>
                width={18}
                options={GC_PRODUCTS}
                value={productOption}
                onChange={this.onProductChange}
              />
            }
          />
        </div>

        <div style={{ flex: 1 }}>
          {product === "cdn" && this.renderCdnSection(query)}
          {product === "dns" && this.renderDnsSection(query)}
          {product === "fastedge" && this.renderFastEdgeSection(query)}
          {product === "waap" && this.renderWaapSection(query)}
        </div>
      </div>
    );
  }

  renderCdnSection(query: GCQuery) {
    const {
      metric,
      legendFormat,
      grouping,
      countries,
      granularity,
      regions,
      vhosts,
      resources,
      clients,
    } = defaults(query, defaultQuery);

    return (
      <>
        <div>
          <div className="section" style={{ marginRight: "27px" }}>
            <label className="gf-form-group-label">Query Props</label>
            <div className="gf-form">
              <FormField
                label="Metric"
                tooltip="Metric to query (traffic, cache, requests, responses, etc.)"
                inputEl={
                  <GCSelectMetric
                    width={20}
                    isSearchable
                    maxVisibleValues={20}
                    minMenuHeight={45}
                    menuPlacement="bottom"
                    onChange={this.onMetricChange}
                    value={metric}
                  />
                }
              />
            </div>
            <div className="gf-form">
              <FormField
                label="Granularity"
                tooltip="Time series granularity"
                inputEl={
                  <GCSelectGranularity
                    width={20}
                    maxVisibleValues={4}
                    minMenuHeight={25}
                    menuPlacement="bottom"
                    onChange={this.onGranularityChange}
                    value={granularity}
                  />
                }
              />
            </div>
            <div className="gf-form">
              <FormField
                label="Group By"
                tooltip="Fields used for grouping"
                inputEl={
                  <GCSelectGrouping
                    width={20}
                    isSearchable
                    maxVisibleValues={20}
                    minMenuHeight={35}
                    menuPlacement="bottom"
                    onChange={this.onGroupingChange}
                    value={grouping}
                  />
                }
              />
            </div>
          </div>
          <div className="section" style={{ marginRight: "27px" }}>
            <label className="gf-form-group-label">Filters (comma separated)</label>
            <div className="gf-form">
              <GCInput width={8} value={vhosts} onChange={this.onVhostsChange} label="Vhosts" tooltip="Filter by vhost" type="text" />
            </div>
            <div className="gf-form">
              <GCInput width={8} value={resources} onChange={this.onResourcesChange} label="Resources" tooltip="Filter by resource id" type="text" />
            </div>
            <div className="gf-form">
              <GCInput width={8} value={clients} onChange={this.onClientsChange} label="Clients" tooltip="Filter by client id" type="text" />
            </div>
            <div className="gf-form">
              <GCInput width={8} value={regions} onChange={this.onRegionChange} label="Regions" tooltip="Filter by region" type="text" />
            </div>
            <div className="gf-form">
              <GCInput width={8} value={countries} onChange={this.onCountryChange} label="Countries" tooltip="Filter by country" type="text" />
            </div>
          </div>
        </div>
        <div className="gf-form">
          <GCInput
            inputWidth={30}
            value={legendFormat}
            onChange={this.onLegendFormatChange}
            label="Legend"
            placeholder="legend format"
            tooltip="Controls the name of the time series."
            type="text"
          />
        </div>
      </>
    );
  }

  renderDnsSection(query: GCQuery) {
    const q = defaults(query, defaultDNSQuery) as GCQuery;
    const zone = q.zone ?? "all";
    const selectedZone = zone === "all" ? { value: "all", label: "All Zones" } : this.state.dnsZones.find((z) => z.value === zone) ?? { value: zone, label: zone };
    const dnsGranularity = q.dnsGranularity ?? { value: GCDNSGranularity.FiveMinutes, label: "5m" };
    const recordType = q.record_type ?? GCDNSRecordType.All;

    return (
      <div style={{ display: "flex", flexDirection: "column", gap: "12px", maxWidth: "420px" }}>
        <label className="gf-form-group-label">Query Props</label>
        <FormField
          label="Zone"
          labelWidth={10}
          tooltip="Select a DNS Zone"
          inputEl={
            <Select
              width={20}
              options={this.state.dnsZones}
              isLoading={this.state.dnsZonesLoading}
              isSearchable
              menuPlacement="bottom"
              onChange={this.onZoneChange}
              value={selectedZone}
              placeholder={this.state.dnsZonesLoading ? "Loading zones..." : "Select zone"}
            />
          }
        />
        <FormField
          label="Granularity"
          labelWidth={10}
          tooltip="DNS statistics aggregation interval"
          inputEl={
            <Select
              width={20}
              options={DNS_GRANULARITY_OPTS}
              value={DNS_GRANULARITY_OPTS.find((o) => o.value === (dnsGranularity?.value ?? dnsGranularity)) ?? DNS_GRANULARITY_OPTS[0]}
              onChange={this.onDnsGranularityChange}
            />
          }
        />
        <FormField
          label="Record type"
          labelWidth={10}
          tooltip="DNS record type to include in statistics"
          inputEl={
            <Select
              width={20}
              options={DNS_RECORD_TYPE_OPTS}
              value={DNS_RECORD_TYPE_OPTS.find((o) => o.value === recordType) ?? DNS_RECORD_TYPE_OPTS[0]}
              onChange={this.onRecordTypeChange}
            />
          }
        />
        <GCInput
          inputWidth={30}
          value={q.dnsLegendFormat ?? ""}
          onChange={this.onDnsLegendFormatChange}
          label="Legend"
          placeholder="e.g. zone, record_type"
          tooltip="Controls how DNS time series are named in the legend."
          type="text"
        />
      </div>
    );
  }

  renderFastEdgeSection(query: GCQuery) {
    const q = defaults(query, defaultFastEdgeQuery) as GCQuery;
    const appId = q.appId ?? 0;
    const step = q.step ?? 60;
    const selectedApp = this.state.fastedgeApps.find((a) => a.value === appId) ?? this.state.fastedgeApps[0];
    const metric = q.fastedgeMetric ?? "avg";

    return (
      <div style={{ display: "flex", flexDirection: "column", gap: "16px", maxWidth: "420px" }}>
        <label className="gf-form-group-label">Query Props</label>
        <FormField
          label="App Name"
          labelWidth={12}
          tooltip="FastEdge application to query"
          inputEl={
            this.state.fastedgeAppsLoading ? (
              <Spinner />
            ) : (
              <Select
                options={this.state.fastedgeApps}
                value={selectedApp}
                onChange={this.onAppChange}
                placeholder="Select an App"
                width={30}
              />
            )
          }
        />
        <FormField
          label="Metric"
          labelWidth={12}
          tooltip="Latency statistic to display"
          inputEl={
            <Select
              options={FASTEDGE_METRIC_OPTS}
              value={FASTEDGE_METRIC_OPTS.find((o) => o.value === metric) ?? FASTEDGE_METRIC_OPTS[0]}
              onChange={this.onFastedgeMetricChange}
              width={20}
            />
          }
        />
        <FormField
          label="Step (s)"
          labelWidth={12}
          tooltip="Sampling interval in seconds"
          inputEl={
            <input
              className="gf-form-input"
              type="number"
              value={String(step)}
              onChange={this.onStepChange}
              placeholder="Step in seconds"
            />
          }
        />
        <FormField
          label="Network"
          labelWidth={12}
          tooltip="Optional FastEdge network name"
          inputEl={
            <input
              className="gf-form-input"
              type="text"
              value={q.network ?? ""}
              onChange={this.onNetworkChange}
              placeholder="Network name"
            />
          }
        />
      </div>
    );
  }

  renderWaapSection(query: GCQuery) {
    const q = defaults(query, defaultWAAPQuery) as GCQuery;
    const metric = q.waapMetric ?? "total_requests";
    const granularity = q.waapGranularity ?? "1h";

    return (
      <div style={{ display: "flex", flexDirection: "column", gap: "16px", maxWidth: "520px" }}>
        <label className="gf-form-group-label">Query Props</label>
        <FormField
          label="Metric"
          labelWidth={10}
          tooltip="WAAP metric to query (for example total_requests or total_bytes)"
          inputEl={
            <Select
              options={WAAP_METRIC_OPTS}
              value={WAAP_METRIC_OPTS.find((o) => o.value === metric) ?? WAAP_METRIC_OPTS[0]}
              onChange={this.onWaapMetricChange}
              width={30}
            />
          }
        />
        <FormField
          label="Granularity"
          labelWidth={10}
          tooltip="Aggregation interval for WAAP statistics"
          inputEl={
            <Select
              options={WAAP_GRANULARITY_OPTS}
              value={WAAP_GRANULARITY_OPTS.find((o) => o.value === granularity) ?? WAAP_GRANULARITY_OPTS[0]}
              onChange={this.onWaapGranularityChange}
              width={30}
            />
          }
        />
        <FormField
          label="Legend Format"
          labelWidth={10}
          tooltip="Controls how WAAP time series are named in the legend."
          inputEl={
            <input
              className="gf-form-input"
              type="text"
              value={q.waapLegendFormat ?? ""}
              onChange={this.onWaapLegendFormatChange}
              placeholder="Legend format"
            />
          }
        />
      </div>
    );
  }
}
